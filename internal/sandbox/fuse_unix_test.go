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
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestResolveFuseMounts(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "root")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Structure:
	// root/
	//   target/ (real dir)
	//   link -> target
	// outside/
	//   target/
	//   link -> target

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
	outLink := filepath.Join(outsideDir, "link")
	if err := os.Symlink(outTarget, outLink); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		isFuse bool
		root   string
		exe    string
		mounts []Mount
		want   []Mount
	}{
		{
			name:   "NotFuse",
			isFuse: false,
			root:   rootDir,
			mounts: []Mount{{Path: linkPath, Writable: true}},
			want:   []Mount{{Path: linkPath, Writable: true}},
		},
		{
			name:   "FuseNoSymlink",
			isFuse: true,
			root:   rootDir,
			mounts: []Mount{{Path: targetPath, Writable: true}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: true},
			},
		},
		{
			name:   "FuseSymlink",
			isFuse: true,
			root:   rootDir,
			mounts: []Mount{{Path: linkPath, Writable: true}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: true},
				{Path: targetPath, Dest: linkPath, Writable: true},
			},
		},
		{
			name: "ExeSymlink",
			// Checks that if the executable itself is a symlink, it is resolved
			// and mounted read-only (as executables are by default in the sandbox).
			isFuse: true,
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
			isFuse: true,
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
			isFuse: true,
			root:   rootDir,
			mounts: []Mount{{Path: linkPath, Writable: false}},
			want: []Mount{
				{Path: targetPath, Dest: targetPath, Writable: false},
				{Path: targetPath, Dest: linkPath, Writable: false},
			},
		},
		{
			name: "OutsideRoot",
			// Checks that mounts outside the root directory are ignored by the resolution logic
			// and mounted as-is.
			isFuse: true,
			root:   rootDir,
			mounts: []Mount{{Path: outLink, Writable: true}},
			// We expect this NOT to proceed to resolution, passing back the original mount
			// because it is outside the root.
			want: []Mount{{Path: outLink, Dest: outLink, Writable: true}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkFuse := func(path string) bool {
				return test.isFuse
			}
			got := resolveFuseMountsImpl(checkFuse, test.root, test.exe, test.mounts)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("resolveFuseMounts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
