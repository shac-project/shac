// Copyright 2026 The Shac Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows

package sandbox

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/shac-project/shac/internal/execsupport"
)

func TestResolveFuseMounts(t *testing.T) {
	// Make sure to evaluate symlinks before comparing, since
	// `resolveMounts` evaluates symlinks.
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootDir := filepath.Join(tmpDir, "root")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Structure:
	// root/
	//   target/ (real dir)
	//   link -> target
	//   link_outside -> outside/target
	// outside/
	//   target/
	//   link -> root/target

	targetPath := filepath.Join(rootDir, "target")
	if err := os.Mkdir(targetPath, 0755); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(rootDir, "link")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatal(err)
	}

	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.Mkdir(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}

	outTarget := filepath.Join(outsideDir, "target")
	if err := os.Mkdir(outTarget, 0755); err != nil {
		t.Fatal(err)
	}

	linkOutsidePath := filepath.Join(rootDir, "link_outside")
	if err := os.Symlink(outTarget, linkOutsidePath); err != nil {
		t.Fatal(err)
	}

	// Link outside root pointing to inside root.
	outLink := filepath.Join(outsideDir, "link")
	if err := os.Symlink(targetPath, outLink); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		root   string
		exe    string
		mounts []Mount
		want   []Mount
	}{
		{
			name:   "LinkInsideRoot",
			root:   rootDir,
			mounts: []Mount{{Path: linkPath, Writable: true}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: true},
				{Path: targetPath, Dest: linkPath, Writable: true},
			},
		},
		{
			name:   "LinkOutsideRoot",
			root:   rootDir,
			mounts: []Mount{{Path: linkOutsidePath, Writable: true}},
			want: []Mount{
				{Path: outTarget, Dest: outTarget, Writable: true},
				{Path: outTarget, Dest: linkOutsidePath, Writable: true},
			},
		},
		{
			name:   "NoSymlink",
			root:   rootDir,
			mounts: []Mount{{Path: targetPath, Writable: true}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: true},
			},
		},
		{
			name: "ExeSymlink",
			// Checks that if the executable itself is a symlink, it is resolved
			// and mounted read-only (as executables are by default in the sandbox).
			root:   rootDir,
			exe:    linkPath,
			mounts: []Mount{},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: false},
				{Path: targetPath, Dest: linkPath, Writable: false},
			},
		},
		{
			name: "ReadOnlyNoSymlink",
			// Checks that a read-only mount of a regular directory is preserved as read-only.
			root:   rootDir,
			mounts: []Mount{{Path: targetPath, Writable: false}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: false},
			},
		},
		{
			name: "ReadOnlySymlink",
			// Checks that a read-only mount of a symlink is resolved to the target,
			// and both the target and the symlink overlay are mounted read-only.
			root:   rootDir,
			mounts: []Mount{{Path: linkPath, Writable: false}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: false},
				{Path: targetPath, Dest: linkPath, Writable: false},
			},
		},
		{
			name: "OutsideRoot",
			// Checks that mounts outside the root directory are resolved correctly.
			root:   rootDir,
			mounts: []Mount{{Path: outLink, Writable: true}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: true},
				{Path: targetPath, Dest: outLink, Writable: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolveMounts(test.root, test.exe, test.mounts)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("resolveFuseMounts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCanUseNsjail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if canUseNsjail(filepath.Join(dir, "nonexistent")) {
		t.Fatal("expected canUseNsjail to return false for nonexistent path")
	}

	writablePath := filepath.Join(dir, "writable")
	if err := os.WriteFile(writablePath, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !canUseNsjail(writablePath) {
		t.Fatal("expected canUseNsjail to return true for writable path")
	}
}

func TestNewSandbox_InsideOuterNsjail(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("nsjail is only supported on linux/amd64 and linux/arm64")
	}
	t.Parallel()

	if os.Getenv("SHAC_TEST_IN_OUTER_NSJAIL") == "1" {
		sb, err := New(t.TempDir())
		if err != nil {
			t.Fatalf("New() failed inside outer nsjail: %v", err)
		}
		if _, ok := sb.(genericSandbox); !ok {
			t.Fatalf("expected genericSandbox inside outer nsjail with read-only /proc, got %T", sb)
		}
		return
	}

	outerTmp := t.TempDir()
	outerSb, err := New(outerTmp)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if _, ok := outerSb.(nsjailSandbox); !ok {
		t.Skipf("host environment already uses %T, skipping nested nsjail test", outerSb)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childTmp := t.TempDir()
	cmd := outerSb.Command(t.Context(), &Config{
		Cmd: []string{exe, "-test.run=^TestNewSandbox_InsideOuterNsjail$"},
		Cwd: childTmp,
		Env: map[string]string{
			"SHAC_TEST_IN_OUTER_NSJAIL": "1",
			"TMPDIR":                    childTmp,
		},
		Mounts: []Mount{
			{Path: "/lib"},
			{Path: "/lib64"},
			{Path: "/usr/lib"},
			{Path: childTmp, Writable: true},
		},
	})
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := execsupport.Run(t.Context(), cmd); err != nil {
		t.Fatalf("inner test in outer nsjail failed: %s\noutput:\n%s", err, out.String())
	}
}

// TestNsjailSandbox_DevShm verifies that nsjailSandbox mounts a writable,
// isolated tmpfs at /dev/shm inside the sandbox. It re-executes the test binary
// inside nsjail (with SHAC_TEST_DEV_SHM_INNER=1) to write a file under
// /dev/shm, then checks from the outer process that the write succeeded inside
// the sandbox without creating the file in the host's /dev/shm.
func TestNsjailSandbox_DevShm(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("nsjail is only supported on linux/amd64 and linux/arm64")
	}
	t.Parallel()

	if os.Getenv("SHAC_TEST_DEV_SHM_INNER") == "1" {
		shmFile := os.Getenv("SHAC_TEST_DEV_SHM_FILE")
		if err := os.WriteFile(shmFile, []byte("isolated"), 0o600); err != nil {
			t.Fatalf("failed to write to %s inside nsjail: %s", shmFile, err)
		}
		return
	}

	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() failed: %s", err)
	}
	if _, ok := sb.(nsjailSandbox); !ok {
		t.Skipf("host environment uses %T, skipping nsjail /dev/shm test", sb)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childTmp := t.TempDir()
	shmFile := filepath.Join("/dev/shm", "shac-test-"+filepath.Base(childTmp))
	t.Cleanup(func() {
		_ = os.Remove(shmFile)
	})

	cmd := sb.Command(t.Context(), &Config{
		Cmd: []string{exe, "-test.run=^TestNsjailSandbox_DevShm$"},
		Cwd: childTmp,
		Env: map[string]string{
			"SHAC_TEST_DEV_SHM_INNER": "1",
			"SHAC_TEST_DEV_SHM_FILE":  shmFile,
			"TMPDIR":                  childTmp,
		},
		Mounts: []Mount{
			// Include /dev as a read-only mount to verify that the ephemeral
			// /dev/shm tmpfs is mounted after bind mounts and is not shadowed.
			{Path: "/dev"},
			{Path: "/lib"},
			{Path: "/lib64"},
			{Path: "/usr/lib"},
			{Path: childTmp, Writable: true},
		},
	})
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := execsupport.Run(t.Context(), cmd); err != nil {
		t.Fatalf("inner test in nsjail failed: %s\noutput:\n%s", err, out.String())
	}
	if _, err := os.Stat(shmFile); !os.IsNotExist(err) {
		t.Fatalf("expected %s not to exist on host /dev/shm, got err=%v", shmFile, err)
	}
}

// TestNsjailSandbox_RelativeSymlinkExe verifies that nsjail can execute a
// binary via a relative symlink inside a mounted directory whose target lives
// in a sibling directory that sorts later alphabetically (such as
// /usr/bin/echo -> ../lib/cargo/bin/coreutils/echo on Ubuntu 26.04).
func TestNsjailSandbox_RelativeSymlinkExe(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("nsjail is only supported on linux/amd64 and linux/arm64")
	}
	t.Parallel()

	if os.Getenv("SHAC_TEST_SYMLINK_EXE_INNER") == "1" {
		return
	}

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sb, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() failed: %s", err)
	}
	if _, ok := sb.(nsjailSandbox); !ok {
		t.Skipf("host environment uses %T, skipping nsjail symlink exe test", sb)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	usrBin := filepath.Join(tmpDir, "usr", "bin")
	usrLib := filepath.Join(tmpDir, "usr", "lib")
	coreutilsDir := filepath.Join(usrLib, "cargo", "bin", "coreutils")
	if err := os.MkdirAll(usrBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(coreutilsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realExe := filepath.Join(coreutilsDir, "test-bin")
	if err := os.Link(exe, realExe); err != nil {
		data, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		execsupport.Mu.Lock()
		err = os.WriteFile(realExe, data, 0o755)
		execsupport.Mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	linkExe := filepath.Join(usrBin, "test-bin")
	if err := os.Symlink("../lib/cargo/bin/coreutils/test-bin", linkExe); err != nil {
		t.Fatal(err)
	}
	binLink := filepath.Join(tmpDir, "bin")
	if err := os.Symlink("usr/bin", binLink); err != nil {
		t.Fatal(err)
	}
	libLink := filepath.Join(tmpDir, "lib")
	if err := os.Symlink("usr/lib", libLink); err != nil {
		t.Fatal(err)
	}

	for _, cmdPath := range []string{linkExe, filepath.Join(binLink, "test-bin")} {
		workDir := t.TempDir()
		cmd := sb.Command(t.Context(), &Config{
			Cmd: []string{cmdPath, "-test.run=^TestNsjailSandbox_RelativeSymlinkExe$"},
			Cwd: workDir,
			Env: map[string]string{
				"SHAC_TEST_SYMLINK_EXE_INNER": "1",
				"TMPDIR":                      workDir,
			},
			Mounts: []Mount{
				{Path: "/lib"},
				{Path: "/lib64"},
				{Path: "/usr/lib"},
				{Path: binLink},
				{Path: usrBin},
				{Path: libLink},
				{Path: usrLib},
				{Path: workDir, Writable: true},
			},
		})
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := execsupport.Run(t.Context(), cmd); err != nil {
			t.Fatalf("executing %s in nsjail failed: %s\noutput:\n%s", cmdPath, err, out.String())
		}
	}
}
