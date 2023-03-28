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

func TestLoad_SCM_Affected_Files_Raw(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file1.txt", "First file")
	copyFile(t, root, "scm_affected_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star", false); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_affected_files.star:7] {\"file1.txt\": file(action = \"\"), \"scm_affected_files.star\": file(action = \"\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_SCM_All_Files_Raw(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file1.txt", "First file")
	copyFile(t, root, "scm_all_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_all_files.star", false); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_all_files.star:7] {\"file1.txt\": file(action = \"\"), \"scm_all_files.star\": file(action = \"\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_SCM_Affected_Files_Git_All(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_affected_files.star")
	runGit(t, root, "add", "scm_affected_files.star")
	runGit(t, root, "commit", "-m", "Third commit")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star", true); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_affected_files.star:7] {\"file1.txt\": file(action = \"\"), \"file2.txt\": file(action = \"\"), \"scm_affected_files.star\": file(action = \"\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
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
	if err := Load(context.Background(), root, "scm_affected_files.star", false); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_affected_files.star:7] {\"file2.txt\": file(action = \"A\"), \"scm_affected_files.star\": file(action = \"A\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_SCM_Affected_Files_Git_NoUpstream_Tainted(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_affected_files.star")
	runGit(t, root, "add", "scm_affected_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star", false); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_affected_files.star:7] {\"scm_affected_files.star\": file(action = \"A\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_SCM_Affected_Files_Git_NoUpstream_Pristine(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_affected_files.star")
	runGit(t, root, "add", "scm_affected_files.star")
	runGit(t, root, "commit", "-m", "Third commit")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_affected_files.star", false); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_affected_files.star:7] {\"scm_affected_files.star\": file(action = \"A\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_SCM_All_Files_Git(t *testing.T) {
	root := makeGit(t)
	copyFile(t, root, "scm_all_files.star")
	runGit(t, root, "add", "scm_all_files.star")
	b := getErrPrint(t)
	if err := Load(context.Background(), root, "scm_all_files.star", false); err != nil {
		t.Fatal(err)
	}
	want := "[//scm_all_files.star:7] {\"file1.txt\": file(action = \"\"), \"file2.txt\": file(action = \"\"), \"scm_all_files.star\": file(action = \"\")}\n"
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

// TestTestDataFail runs all the files under testdata/fail/.
func TestTestDataFail(t *testing.T) {
	t.Parallel()
	p, got := enumDir(t, "fail")
	inexistant, err := filepath.Abs(filepath.Join("testdata", "fail", "inexistant"))
	if err != nil {
		t.Fatal(err)
	}
	// TODO(maruel): Fix the error to include the call site when applicable.
	data := []struct {
		name  string
		err   string
		trace string
	}{
		{
			"backtrace.star",
			"inner",
			`  //backtrace.star:11:4: in <toplevel>
  //backtrace.star:9:6: in fn1
  //backtrace.star:6:7: in fn2
  <builtin>: in fail
Error: inner`,
		},
		{
			"empty.star",
			"did you forget to call register_check?",
			"",
		},
		{
			"fail.star",
			"an expected failure",
			`  //fail.star:5:5: in <toplevel>
  <builtin>: in fail
Error: an expected failure`,
		},
		{
			"io_read_file_abs.star",
			"do not use absolute path",
			"",
		},
		{
			"io_read_file_escape.star",
			"cannot escape root",
			"",
		},
		{
			"io_read_file_inexistant.star",
			"open " + inexistant + ": no such file or directory",
			"",
		},
		{
			"io_read_file_missing_arg.star",
			"read_file: got 0 arguments, want 1",
			"",
		},
		{
			"io_read_file_unclean.star",
			"pass cleaned path",
			"",
		},
		{
			"io_read_file_windows.star",
			"use POSIX style path",
			"",
		},
		{
			"re_allmatches_no_arg.star",
			"allmatches: missing argument for pattern",
			"",
		},
		{
			"re_match_bad_re.star",
			"error parsing regexp: missing closing ): `(`",
			"",
		},
		{
			"re_match_no_arg.star",
			"match: missing argument for pattern",
			"",
		},
		{
			"register_check_kwargs.star",
			"register_check: unexpected keyword arguments",
			`  //register_check_kwargs.star:8:15: in <toplevel>
Error in register_check: register_check: unexpected keyword arguments`,
		},
		{
			"register_check_no_arg.star",
			"register_check: got 0 arguments, want 1",
			`  //register_check_no_arg.star:5:15: in <toplevel>
Error in register_check: register_check: got 0 arguments, want 1`,
		},
		{
			"register_check_recursive.star",
			"can't register checks after done loading",
			"",
		},
		{
			"scm_affected_files_arg.star",
			"affected_files: unexpected arguments",
			"",
		},
		{
			"scm_affected_files_kwarg.star",
			"affected_files: unexpected keyword arguments",
			"",
		},
		{
			"scm_all_files_arg.star",
			"all_files: unexpected arguments",
			"",
		},
		{
			"scm_all_files_kwarg.star",
			"all_files: unexpected keyword arguments",
			"",
		},
		{
			"syntax_error.star",
			"//syntax_error.star:5:3: got '//', want primary expression",
			"",
		},
		{
			"undefined_symbol.star",
			"//undefined_symbol.star:5:1: undefined: undefined_symbol",
			"",
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
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			err := Load(context.Background(), p, data[i].name, false)
			if err == nil {
				t.Fatal("expecting an error")
			}
			if diff := cmp.Diff(data[i].err, err.Error()); diff != "" {
				t.Fatalf("mismatch (+want -got):\n%s", diff)
			}
			err = unwrapMultiError(t, err)
			expectTrace := data[i].trace != ""
			var err2 BacktracableError
			if errors.As(err, &err2) != expectTrace {
				if expectTrace {
					t.Fatal("expected backtracable error")
				} else {
					t.Fatalf("unexpected backtracable error: %s", err2.Backtrace())
				}
			}
			if expectTrace {
				if diff := cmp.Diff("Traceback (most recent call last):\n"+data[i].trace, err2.Backtrace()); diff != "" {
					t.Fatalf("mismatch (+want -got):\n%s", diff)
				}
			}
		})
	}
}

// TestTestDataSimple runs all the files under testdata/simple/.
func TestTestDataSimple(t *testing.T) {
	p, got := enumDir(t, "simple")
	v := fmt.Sprintf("(%d, %d, %d)", version[0], version[1], version[2])
	data := []struct {
		name string
		want string
	}{
		{
			"dir_native.star",
			"[//dir_native.star:5] [\"commitHash\", \"version\"]\n",
		},
		{
			"dir_shac.star",
			"[//dir_shac.star:6] [\"exec\", \"io\", \"re\", \"result\", \"scm\"]\n",
		},
		{
			"io_read_file.star",
			"[//io_read_file.star:7] {\"key\": \"value\"}\n",
		},
		{
			"minimal.star",
			"[//minimal.star:5] " + v + "\n",
		},
		{
			"re_allmatches.star",
			`[//re_allmatches.star:7] ()
[//re_allmatches.star:9] (match(groups = ("TODO(foo)",), offset = 4), match(groups = ("TODO(bar)",), offset = 14))
[//re_allmatches.star:11] (match(groups = ("anc", "n", "c"), offset = 0),)
`,
		},
		{
			"re_match.star",
			`[//re_match.star:7] None
[//re_match.star:9] match(groups = ("TODO(foo)",), offset = 4)
[//re_match.star:11] match(groups = ("anc", "n", "c"), offset = 0)
`,
		},
		{
			"register_check.star",
			"[//register_check.star:6] running\n",
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
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			b := getErrPrint(t)
			if err := Load(context.Background(), p, data[i].name, false); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(data[i].want, b.String()); diff != "" {
				t.Fatalf("mismatch (+want -got):\n%s", diff)
			}
		})
	}
}

// Utilities

func unwrapMultiError(t *testing.T, err error) error {
	// TODO(maruel): Use go 1.20 unwrap.
	var errs errors.MultiError
	if !errors.As(err, &errs) {
		return err
	}
	if len(errs) != 1 {
		t.Fatal("expected one wrapped error")
	}
	return errs[0]
}

func enumDir(t *testing.T, name string) (string, []string) {
	p := filepath.Join("testdata", name)
	d, err := os.ReadDir(p)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(d))
	for i := range d {
		n := d[i].Name()
		if strings.HasSuffix(n, ".star") && !d[i].IsDir() {
			out = append(out, n)
		}
	}
	return p, out
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
