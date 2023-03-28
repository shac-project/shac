// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.chromium.org/luci/common/errors"
)

func TestLoad_Backtrace(t *testing.T) {
	err := Load(context.Background(), "testdata", "backtrace.star")
	if err == nil {
		t.Fatal("expected a failure")
	}
	if s := err.Error(); s != "inner" {
		t.Fatal(s)
	}
	var errs errors.MultiError
	if !errors.As(err, &errs) {
		t.Fatal("not a MultiError")
	}
	if len(errs) != 1 {
		t.Fatal("expected one wrapped error")
	}
	var err2 BacktracableError
	if !errors.As(errs[0], &err2) {
		t.Fatal("not a backtracable error")
	}
	want := `Traceback (most recent call last):
  //backtrace.star:11:4: in <toplevel>
  //backtrace.star:9:6: in fn1
  //backtrace.star:6:7: in fn2
  <builtin>: in fail
Error: inner`
	if diff := cmp.Diff(want, err2.Backtrace()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_Dir_Native(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "dir_native.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//dir_native.star:5] [\"commitHash\", \"version\"]\n" {
		t.Fatal(s)
	}
}

func TestLoad_Dir_Shac(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "dir_shac.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//dir_shac.star:6] [\"exec\", \"io\", \"result\", \"scm\"]\n" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "io_read_file.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//io_read_file.star:7] {\"key\": \"value\"}\n" {
		t.Fatal(s)
	}
}

func TestLoad_Minimal(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "minimal.star"); err != nil {
		t.Fatal(err)
	}
	v := fmt.Sprintf("(%d, %d, %d)", version[0], version[1], version[2])
	if s := b.String(); s != "[//minimal.star:5] "+v+"\n" {
		t.Fatal(s)
	}
}

func TestLoad_Register_Check(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "register_check.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//register_check.star:6] running\n" {
		t.Fatal(s)
	}
}

func TestLoad_Register_Check_Recursive(t *testing.T) {
	if err := Load(context.Background(), "testdata", "register_check_recursive.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "can't register checks after done loading" {
		t.Fatal(s)
	}
}

func makeGit(t *testing.T) string {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, "file1.txt", "First file")
	runGit(t, root, "add", "file1.txt")
	runGit(t, root, "commit", "-m", "Initial commit")
	// TODO(maruel): scm.go requires two commits. Not really worth fixing yet,
	// it's only annoying in unit tests.
	writeFile(t, root, "file2.txt", "First file")
	runGit(t, root, "add", "file2.txt")
	runGit(t, root, "commit", "-m", "Second commit")
	return root
}

func copyFile(t *testing.T, dst, path string) {
	d, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dst, path, string(d))
}

func writeFile(t *testing.T, root, path, content string) {
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	// First is for git version before 2.32, the rest are to skip the user and system config.
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOGLOBAL=true", "GIT_CONFIG_GLOBAL=", "GIT_CONFIG_SYSTEM=", "EMAIL=test@example.com", "GIT_AUTHOR_DATE=2000-01-01T00:00:00", "GIT_COMMITTER_DATE=2000-01-01T00:00:00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestLoad_SCM_Affected_Files_Raw(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file1.txt", "First file")
	copyFile(t, root, "scm_affected_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//scm_affected_files.star:7] {\"file1.txt\": {}, \"scm_affected_files.star\": {}}\n" {
		t.Fatal(s)
	}
}

func TestLoad_SCM_All_Files_Raw(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file1.txt", "First file")
	copyFile(t, root, "scm_all_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_all_files.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//scm_all_files.star:7] {\"file1.txt\": {}, \"scm_all_files.star\": {}}\n" {
		t.Fatal(s)
	}
}

func TestLoad_SCM_Affected_Files_Git_Upstream_Tainted(t *testing.T) {
	root := makeGit(t)
	// Setup an upstream being the root commit.
	runGit(t, root, "checkout", "-b", "up", "HEAD~1")
	runGit(t, root, "checkout", "master")
	runGit(t, root, "branch", "--set-upstream-to", "up")
	copyFile(t, root, "scm_affected_files.star")
	runGit(t, root, "add", "scm_affected_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//scm_affected_files.star:7] {\"file2.txt\": {}, \"scm_affected_files.star\": {}}\n" {
		t.Fatal(s)
	}
}

func TestLoad_SCM_Affected_Files_Git_NoUpstream_Tainted(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_affected_files.star")
	runGit(t, root, "add", "scm_affected_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//scm_affected_files.star:7] {\"scm_affected_files.star\": {}}\n" {
		t.Fatal(s)
	}
}

func TestLoad_SCM_Affected_Files_Git_NoUpstream_Pristine(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_affected_files.star")
	runGit(t, root, "add", "scm_affected_files.star")
	runGit(t, root, "commit", "-m", "Third commit")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//scm_affected_files.star:7] {\"scm_affected_files.star\": {}}\n" {
		t.Fatal(s)
	}
}

func TestLoad_SCM_All_Files_Git(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_all_files.star")
	runGit(t, root, "add", "scm_all_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_all_files.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//scm_all_files.star:7] {\"file1.txt\": {}, \"file2.txt\": {}, \"scm_all_files.star\": {}}\n" {
		t.Fatal(s)
	}
}

// TestTestDataFail runs all the files under testdata/fail/.
func TestTestDataFail(t *testing.T) {
	p := filepath.Join("testdata", "fail")
	d, err := os.ReadDir(p)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(d))
	for i := range d {
		if !d[i].IsDir() {
			got[i] = d[i].Name()
		}
	}
	inexistant, err := filepath.Abs(filepath.Join("testdata", "fail", "inexistant"))
	if err != nil {
		t.Fatal(err)
	}
	data := []struct {
		name string
		err  error
	}{
		{
			"empty.star",
			errors.New("did you forget to call register_check?"),
		},
		{
			"fail.star",
			errors.New("an expected failure"),
		},
		{
			"io_read_file_abs.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("do not use absolute path"),
		},
		{
			"io_read_file_escape.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("cannot escape root"),
		},
		{
			"io_read_file_inexistant.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("open " + inexistant + ": no such file or directory"),
		},
		{
			"io_read_file_missing_arg.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("read_file: got 0 arguments, want 1"),
		},
		{
			"io_read_file_unclean.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("pass cleaned path"),
		},
		{
			"io_read_file_windows.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("use POSIX style path"),
		},
		{
			"register_check_kwargs.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("register_check: unexpected keyword arguments"),
		},
		{
			"register_check_no_arg.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("register_check: got 0 arguments, want 1"),
		},
		{
			"syntax_error.star",
			errors.New("//syntax_error.star:5:3: got '//', want primary expression"),
		},
		{
			"undefined_symbol.star",
			errors.New("//undefined_symbol.star:5:1: undefined: undefined_symbol"),
		},
	}
	want := make([]string, len(data))
	for i := range data {
		want[i] = data[i].name
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
	for i := range data {
		t.Run(data[i].name, func(t *testing.T) {
			err := Load(context.Background(), p, data[i].name)
			if !equalError(data[i].err, err) {
				if diff := cmp.Diff(data[i].err, err); diff != "" {
					t.Fatalf("mismatch (+want -got):\n%s", diff)
				}
			}
		})
	}
}

func equalError(a, b error) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Error() == b.Error()
}

func getErrPrint(t *testing.T) *bytes.Buffer {
	old := stderrPrint
	t.Cleanup(func() {
		stderrPrint = old
	})
	b := &bytes.Buffer{}
	stderrPrint = b
	return b
}

type panicOnWrite struct{}

func (panicOnWrite) Write([]byte) (int, error) {
	panic("unexpected write")
}

func init() {
	// Catch unexpected stderrPrint usage.
	stderrPrint = panicOnWrite{}
	// Silence logging.
	log.SetOutput(io.Discard)
}
