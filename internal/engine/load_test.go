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
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.chromium.org/luci/common/errors"
)

func TestLoad_SCM_Raw(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "file1.txt", "First file")
	copySCM(t, root)
	t.Run("affected", func(t *testing.T) {
		want := "[//scm_affected_files.star:9] \n" +
			"file1.txt: \n" +
			"scm_affected_files.star: \n" +
			"scm_affected_files_new_lines.star: \n" +
			"scm_all_files.star: \n" +
			"\n"
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:15] file1.txt\n" +
			"1: First file\n"
		testStarlark(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:9] \n" +
			"file1.txt: \n" +
			"scm_affected_files.star: \n" +
			"scm_affected_files_new_lines.star: \n" +
			"scm_all_files.star: \n" +
			"\n"
		testStarlark(t, root, "scm_all_files.star", false, want)
	})
}

func TestLoad_SCM_Git_NoUpstream_Pristine(t *testing.T) {
	// No upstream branch set, pristine checkout.
	t.Parallel()
	root := makeGit(t)
	copySCM(t, root)
	runGit(t, root, "add", "scm_*.star")
	runGit(t, root, "commit", "-m", "Third commit")
	t.Run("affected", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:9] \n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:9] \n" +
			"file1.txt: A\n" +
			"file2.txt: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_affected_files.star", true, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:9] \n" +
			"file1.txt: A\n" +
			"file2.txt: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_all_files.star", false, want)
	})
}

func TestLoad_SCM_Git_NoUpstream_Staged(t *testing.T) {
	// No upstream branch set, staged changes.
	t.Parallel()
	root := makeGit(t)
	copySCM(t, root)
	runGit(t, root, "add", "scm_*.star")
	t.Run("affected", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:9] \n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:15] scm_affected_files.star\n" +
			"1: # Copyright 2023 The Fuchsia Authors. All rights reserved.\n"
		testStarlark(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:15] file1.txt\n" +
			"1: First file\n"
		testStarlark(t, root, "scm_affected_files_new_lines.star", true, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:9] \n" +
			"file1.txt: A\n" +
			"file2.txt: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_all_files.star", false, want)
	})
}

func TestLoad_SCM_Git_Upstream_Staged(t *testing.T) {
	// Upstream set, staged changes.
	t.Parallel()
	root := makeGit(t)
	runGit(t, root, "checkout", "-b", "up", "HEAD~1")
	runGit(t, root, "checkout", "master")
	runGit(t, root, "branch", "--set-upstream-to", "up")
	copySCM(t, root)
	runGit(t, root, "add", "scm_*.star")
	t.Run("affected", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:9] \n" +
			"file1.txt: R\n" +
			"file2.txt: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:9] \n" +
			"file1.txt: A\n" +
			"file2.txt: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlark(t, root, "scm_all_files.star", false, want)
	})
}

// TestTestDataFail runs all the files under testdata/fail/.
func TestTestDataFail(t *testing.T) {
	t.Parallel()
	p, got := enumDir(t, "fail")
	// TODO(maruel): Fix the error to include the call site when applicable.
	data := []struct {
		name  string
		err   string
		trace string
	}{
		{
			"backtrace.star",
			"inner",
			`  //backtrace.star:11:4: in <toplevel>` + "\n" +
				`  //backtrace.star:9:6: in fn1` + "\n" +
				`  //backtrace.star:6:7: in fn2` + "\n" +
				`  <builtin>: in fail` + "\n" +
				`Error: inner`,
		},
		{
			"empty.star",
			"did you forget to call register_check?",
			"",
		},
		{
			"exec_bad_type_in_args.star",
			"command args must be strings",
			"",
		},
		{
			"exec_command_not_in_path.star",
			func() string {
				if runtime.GOOS == "windows" {
					return `exec: "this-command-does-not-exist": executable file not found in %PATH%`
				}
				return `exec: "this-command-does-not-exist": executable file not found in $PATH`
			}(),
			"",
		},
		{
			"exec_invalid_cwd.star",
			"cannot escape root",
			"",
		},
		{
			"fail.star",
			"an expected failure",
			`  //fail.star:5:5: in <toplevel>` + "\n" +
				`  <builtin>: in fail` + "\n" +
				`Error: an expected failure`,
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
			func() string {
				inexistant, err := filepath.Abs(filepath.Join("testdata", "fail"))
				if err != nil {
					t.Fatal(err)
				}
				// Work around the fact that path are not yet correctly handled on
				// Windows.
				inexistant += "/inexistant"
				// TODO(maruel): This error comes from the OS, thus this is a very
				// brittle test case.
				if runtime.GOOS == "windows" {
					return "open " + inexistant + ": The system cannot find the file specified."
				}
				return "open " + inexistant + ": no such file or directory"
			}(),
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
			`  //register_check_kwargs.star:8:15: in <toplevel>` + "\n" +
				`Error in register_check: register_check: unexpected keyword arguments`,
		},
		{
			"register_check_no_arg.star",
			"register_check: got 0 arguments, want 1",
			`  //register_check_no_arg.star:5:15: in <toplevel>` + "\n" +
				`Error in register_check: register_check: got 0 arguments, want 1`,
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
			err := Load(context.Background(), p, data[i].name, false, &reportNoPrint{t: t})
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
	t.Parallel()
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
			"exec_success.star",
			"[//exec_success.star:6] retcode: 0\n",
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
			`[//re_allmatches.star:7] ()` + "\n" +
				`[//re_allmatches.star:9] (match(groups = ("TODO(foo)",), offset = 4), match(groups = ("TODO(bar)",), offset = 14))` + "\n" +
				`[//re_allmatches.star:11] (match(groups = ("anc", "n", "c"), offset = 0),)` + "\n",
		},
		{
			"re_match.star",
			`[//re_match.star:7] None` + "\n" +
				`[//re_match.star:9] match(groups = ("TODO(foo)",), offset = 4)` + "\n" +
				`[//re_match.star:11] match(groups = ("anc", "n", "c"), offset = 0)` + "\n",
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
			testStarlark(t, p, data[i].name, false, data[i].want)
		})
	}
}

// Utilities

func testStarlark(t *testing.T, root, name string, all bool, want string) {
	r := reportPrint{t: t}
	if err := Load(context.Background(), root, name, all, &r); err != nil {
		t.Helper()
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, r.b.String()); diff != "" {
		t.Helper()
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

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
	// scm.go requires two commits. Not really worth fixing yet, it's only
	// annoying in unit tests.
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "engine test")

	writeFile(t, root, "file.txt", "First file\nIt doesn't contain\na lot of lines.\n")
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-m", "Initial commit")

	runGit(t, root, "mv", "file.txt", "file1.txt")
	writeFile(t, root, "file1.txt", "First file\nIt contains\na lot of lines.\n")
	runGit(t, root, "add", "file1.txt")
	writeFile(t, root, "file2.txt", "Second file")
	runGit(t, root, "add", "file2.txt")
	runGit(t, root, "commit", "-m", "Second commit")
	return root
}

func copySCM(t *testing.T, dst string) {
	m, err := filepath.Glob(filepath.Join("testdata", "scm_*.star"))
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range m {
		d, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dst, filepath.Base(src), string(d))
	}
}

func writeFile(t *testing.T, root, path, content string) {
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	// First is for git version before 2.32, the next two are to skip the user
	// and system config on more recent version.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOGLOBAL=true",
		"GIT_CONFIG_GLOBAL=",
		"GIT_CONFIG_SYSTEM=",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00",
		"LANG=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run git %s\n%s\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

type reportNoPrint struct {
	t *testing.T
}

func (r *reportNoPrint) Print(ctx context.Context, file string, line int, message string) {
	r.t.Errorf("unexpected print: %s(%d): %s", file, line, message)
}

type reportPrint struct {
	t *testing.T
	b bytes.Buffer
}

func (r *reportPrint) Print(ctx context.Context, file string, line int, message string) {
	fmt.Fprintf(&r.b, "[%s:%d] %s\n", file, line, message)
}

func init() {
	// Silence logging.
	log.SetOutput(io.Discard)
}
