// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRun_SCM_Raw(t *testing.T) {
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
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:17] file1.txt\n" +
			"1: First file\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:9] \n" +
			"file1.txt: \n" +
			"scm_affected_files.star: \n" +
			"scm_affected_files_new_lines.star: \n" +
			"scm_all_files.star: \n" +
			"\n"
		testStarlarkPrint(t, root, "scm_all_files.star", false, want)
	})
}

func TestRun_SCM_Git_NoUpstream_Pristine(t *testing.T) {
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
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
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
		testStarlarkPrint(t, root, "scm_affected_files.star", true, want)
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
		testStarlarkPrint(t, root, "scm_all_files.star", false, want)
	})
}

func TestRun_SCM_Git_NoUpstream_Staged(t *testing.T) {
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
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:17] scm_affected_files.star\n" +
			"1: # Copyright 2023 The Shac Authors. All rights reserved.\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:17] file1.txt\n" +
			"1: First file\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", true, want)
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
		testStarlarkPrint(t, root, "scm_all_files.star", false, want)
	})
}

func TestRun_SCM_Git_Upstream_Staged(t *testing.T) {
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
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
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
		testStarlarkPrint(t, root, "scm_all_files.star", false, want)
	})
}

func TestRun_SCM_Git_Submodule(t *testing.T) {
	t.Parallel()
	root := makeGit(t)

	submoduleRoot := filepath.Join(root, "submodule")
	if err := os.Mkdir(submoduleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	initGit(t, submoduleRoot)
	runGit(t, submoduleRoot, "commit", "--allow-empty", "-m", "Initial commit")
	runGit(t, root, "submodule", "add", submoduleRoot)

	copySCM(t, root)
	runGit(t, root, "add", "scm_*.star")

	t.Run("affected", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:9] \n" +
			".gitmodules: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})

	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:9] \n" +
			".gitmodules: A\n" +
			"file1.txt: A\n" +
			"file2.txt: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_all_files.star", false, want)
	})
}

func TestRun_SCM_Git_Binary_File(t *testing.T) {
	t.Parallel()
	root := makeGit(t)

	copySCM(t, root)

	// Git considers a file to be binary if it contains a null byte.
	writeFileBytes(t, root, "a.bin", []byte{0, 1, 2, 3})
	runGit(t, root, "add", "a.bin")

	t.Run("affected", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:9] \n" +
			"a.bin: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})

	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		// Only a binary file is touched, no lines should be considered
		// affected.
		want := "[//scm_affected_files_new_lines.star:19] no new lines\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})

	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:19] no new lines\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", true, want)
	})
}

func TestRun_SCM_Git_Broken(t *testing.T) {
	t.Parallel()
	root := makeGit(t)
	// Break the git checkout so getSCM() fails.
	dotGit := filepath.Join(root, ".git")
	err := os.RemoveAll(dotGit)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".git", "broken")
	// On macOS, the path is replaced with the symlink.
	if dotGit, err = filepath.EvalSymlinks(dotGit); err != nil {
		t.Fatal(err)
	}
	// Git reports paths separated with "/" even on Windows.
	dotGit = strings.ReplaceAll(dotGit, string(os.PathSeparator), "/")
	r := reportNoPrint{t: t}
	if err = Run(context.Background(), root, "scm_affected_files.star", false, &r); err == nil {
		t.Fatal("expected error")
	}
	want := "error running git --no-optional-locks rev-parse --show-toplevel: exit status 128\nfatal: invalid gitfile format: " + dotGit + "\n"
	if diff := cmp.Diff(want, err.Error()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

// TestTestDataFailOrThrow runs all the files under testdata/fail_or_throw/.
//
// These test cases call fail() or throw an exception.
func TestTestDataFailOrThrow(t *testing.T) {
	t.Parallel()
	p, got := enumDir(t, "fail_or_throw")
	fail, err := filepath.Abs(filepath.Join("testdata", "fail_or_throw"))
	if err != nil {
		t.Fatal(err)
	}
	data := []struct {
		name  string
		err   string
		trace string
	}{
		{
			"backtrace.star",
			"fail: inner",
			"  //backtrace.star:11:4: in <toplevel>\n" +
				"  //backtrace.star:9:6: in fn1\n" +
				"  //backtrace.star:6:7: in fn2\n",
		},
		{
			"ctx-emit-annotation-kwarg.star",
			"ctx.emit.annotation: unexpected keyword argument \"foo\"",
			"  //ctx-emit-annotation-kwarg.star:6:22: in cb\n",
		},
		{
			"ctx-emit-annotation-level.star",
			"ctx.emit.annotation: a valid level is required, use one of \"notice\", \"warning\" or \"error\"",
			"  //ctx-emit-annotation-level.star:6:22: in cb\n",
		},
		{
			"ctx-emit-annotation-message.star",
			"ctx.emit.annotation: a message is required",
			"  //ctx-emit-annotation-message.star:6:22: in cb\n",
		},
		{
			"ctx-emit-annotation-replacements.star",
			"ctx.emit.annotation: invalid replacements, expect tuple of str",
			"  //ctx-emit-annotation-replacements.star:6:22: in cb\n",
		},
		{
			"ctx-emit-annotation-span-len.star",
			"ctx.emit.annotation: invalid span, expect ((line, col), (line, col))",
			"  //ctx-emit-annotation-span-len.star:6:22: in cb\n",
		},
		{
			"ctx-emit-annotation-span-negative.star",
			"ctx.emit.annotation: invalid span, expect ((line, col), (line, col))",
			"  //ctx-emit-annotation-span-negative.star:6:22: in cb\n",
		},
		{
			"ctx-emit-annotation-span-str.star",
			"ctx.emit.annotation: invalid span, expect ((line, col), (line, col))",
			"  //ctx-emit-annotation-span-str.star:6:22: in cb\n",
		},
		{
			"ctx-immutable.star",
			"can't assign to .key field of struct",
			"  //ctx-immutable.star:7:6: in cb\n",
		},
		{
			"ctx-io-read_file-abs.star",
			"ctx.io.read_file: do not use absolute path",
			"  //ctx-io-read_file-abs.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-dir.star",
			func() string {
				// TODO(maruel): This error comes from the OS, thus this is a very
				// brittle test case.
				if runtime.GOOS == "windows" {
					return "ctx.io.read_file: read " + fail + ": Incorrect function."
				}
				return "ctx.io.read_file: read " + fail + ": is a directory"
			}(),
			"  //ctx-io-read_file-dir.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-escape.star",
			"ctx.io.read_file: cannot escape root",
			"  //ctx-io-read_file-escape.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-inexistant.star",
			func() string {
				// Work around the fact that path are not yet correctly handled on
				// Windows.
				inexistant := fail + "/inexistant"
				// TODO(maruel): This error comes from the OS, thus this is a very
				// brittle test case.
				if runtime.GOOS == "windows" {
					return "ctx.io.read_file: open " + inexistant + ": The system cannot find the file specified."
				}
				return "ctx.io.read_file: open " + inexistant + ": no such file or directory"
			}(),
			"  //ctx-io-read_file-inexistant.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-missing_arg.star",
			"ctx.io.read_file: missing argument for path",
			"  //ctx-io-read_file-missing_arg.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-size_big.star",
			"ctx.io.read_file: invalid size",
			"  //ctx-io-read_file-size_big.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-size_type.star",
			"ctx.io.read_file: for parameter \"size\": got string, want int",
			"  //ctx-io-read_file-size_type.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-unclean.star",
			"ctx.io.read_file: pass cleaned path",
			"  //ctx-io-read_file-unclean.star:6:19: in cb\n",
		},
		{
			"ctx-io-read_file-windows.star",
			"ctx.io.read_file: use POSIX style path",
			"  //ctx-io-read_file-windows.star:6:19: in cb\n",
		},
		{
			"ctx-os-exec-bad_arg.star",
			"ctx.os.exec: unexpected keyword argument \"unknown\"",
			"  //ctx-os-exec-bad_arg.star:6:14: in cb\n",
		},
		{
			"ctx-os-exec-bad_type_in_args.star",
			"ctx.os.exec: command args must be strings",
			"  //ctx-os-exec-bad_type_in_args.star:6:14: in cb\n",
		},
		{
			"ctx-os-exec-command_not_in_path.star",
			func() string {
				if runtime.GOOS == "windows" {
					return "ctx.os.exec: exec: \"this-command-does-not-exist\": executable file not found in %PATH%"
				}
				return "ctx.os.exec: exec: \"this-command-does-not-exist\": executable file not found in $PATH"
			}(),
			"  //ctx-os-exec-command_not_in_path.star:6:14: in cb\n",
		},
		{
			"ctx-os-exec-invalid_cwd.star",
			"ctx.os.exec: cannot escape root",
			"  //ctx-os-exec-invalid_cwd.star:6:14: in cb\n",
		},
		{
			"ctx-os-exec-no_cmd.star",
			"ctx.os.exec: cmdline must not be an empty list",
			"  //ctx-os-exec-no_cmd.star:6:14: in cb\n",
		},
		{
			"ctx-re-allmatches-no_arg.star",
			"ctx.re.allmatches: missing argument for pattern",
			"  //ctx-re-allmatches-no_arg.star:6:20: in cb\n",
		},
		{
			"ctx-re-match-bad_re.star",
			"ctx.re.match: error parsing regexp: missing closing ): `(`",
			"  //ctx-re-match-bad_re.star:6:15: in cb\n",
		},
		{
			"ctx-re-match-no_arg.star",
			"ctx.re.match: missing argument for pattern",
			"  //ctx-re-match-no_arg.star:6:15: in cb\n",
		},
		{
			"ctx-scm-affected_files-arg.star",
			"ctx.scm.affected_files: got 1 arguments, want at most 0",
			"  //ctx-scm-affected_files-arg.star:6:25: in cb\n",
		},
		{
			"ctx-scm-affected_files-kwarg.star",
			"ctx.scm.affected_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-affected_files-kwarg.star:6:25: in cb\n",
		},
		{
			"ctx-scm-all_files-arg.star",
			"ctx.scm.all_files: got 1 arguments, want at most 0",
			"  //ctx-scm-all_files-arg.star:6:20: in cb\n",
		},
		{
			"ctx-scm-all_files-kwarg.star",
			"ctx.scm.all_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-all_files-kwarg.star:6:20: in cb\n",
		},
		{
			"empty.star",
			"did you forget to call shac.register_check?",
			"",
		},
		{
			"fail-check.star",
			"fail: an  unexpected  failure  None\nfail: unexpected keyword argument \"unknown\"",
			"  //fail-check.star:6:7: in cb\n",
		},
		{
			"fail.star",
			"fail: an expected failure",
			"  //fail.star:5:5: in <toplevel>\n",
		},
		{
			"shac-immutable.star",
			"can't assign to .key field of struct",
			"  //shac-immutable.star:6:5: in <toplevel>\n",
		},
		{
			"shac-register_check-builtin.star",
			"shac.register_check: callback must be a function accepting one \"ctx\" argument",
			"  //shac-register_check-builtin.star:5:20: in <toplevel>\n",
		},
		{
			"shac-register_check-callback.star",
			"shac.register_check: callback must be a function accepting one \"ctx\" argument",
			"  //shac-register_check-callback.star:8:20: in <toplevel>\n",
		},
		{
			"shac-register_check-kwarg.star",
			"shac.register_check: unexpected keyword argument \"invalid\"",
			"  //shac-register_check-kwarg.star:8:20: in <toplevel>\n",
		},
		{
			"shac-register_check-no_arg.star",
			"shac.register_check: missing argument for callback",
			"  //shac-register_check-no_arg.star:5:20: in <toplevel>\n",
		},
		{
			"shac-register_check-recursive.star",
			"shac.register_check: can't register checks after done loading",
			"  //shac-register_check-recursive.star:9:22: in cb1\n",
		},
		{
			"shac-register_check-return.star",
			"check \"cb\" returned an object of type string, expected None",
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
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			err := Run(context.Background(), p, data[i].name, false, &reportNoPrint{t: t})
			if err == nil {
				t.Fatal("expecting an error")
			}
			if diff := cmp.Diff(data[i].err, err.Error()); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
			expectTrace := data[i].trace != ""
			var err2 BacktracableError
			if errors.As(err, &err2) != expectTrace {
				if expectTrace {
					t.Fatal("expected BacktracableError")
				} else {
					t.Fatalf("unexpected BacktracableError: %s", err2.Backtrace())
				}
			}
			if expectTrace {
				if diff := cmp.Diff("Traceback (most recent call last):\n"+data[i].trace, err2.Backtrace()); diff != "" {
					t.Fatalf("mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTestDataEmit runs all the files under testdata/emit/.
func TestTestDataEmit(t *testing.T) {
	t.Parallel()
	root, got := enumDir(t, "emit")
	data := []struct {
		name        string
		annotations []annotation
		err         string
	}{
		{
			"ctx-emit-annotation-error.star",
			[]annotation{
				{
					Check:        "cb",
					Level:        "error",
					Message:      "bad code",
					File:         "file.txt",
					Span:         Span{Start: Cursor{Line: 1, Col: 1}, End: Cursor{Line: 10, Col: 1}},
					Replacements: []string{"nothing", "broken code"},
				},
			},
			"a check failed",
		},
		{
			"ctx-emit-annotation.star",
			[]annotation{
				{
					Check:        "cb",
					Level:        "warning",
					Message:      "please fix",
					File:         "file.txt",
					Span:         Span{Start: Cursor{Line: 1, Col: 1}, End: Cursor{Line: 10, Col: 1}},
					Replacements: []string{"nothing", "broken code"},
				},
				{
					Check:        "cb",
					Level:        "notice",
					Message:      "great code",
					Span:         Span{Start: Cursor{Line: 100, Col: 2}, End: Cursor{Line: 100, Col: 2}},
					Replacements: []string{},
				},
			},
			"",
		},
	}
	want := make([]string, len(data))
	for i := range data {
		want[i] = data[i].name
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			r := reportEmit{reportNoPrint: reportNoPrint{t: t}}
			err := Run(context.Background(), root, data[i].name, false, &r)
			if data[i].err != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				got := err.Error()
				if data[i].err != got {
					t.Fatal(got)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(data[i].annotations, r.annotations); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTestDataPrint runs all the files under testdata/print/.
//
// These test cases call print().
func TestTestDataPrint(t *testing.T) {
	t.Parallel()
	p, got := enumDir(t, "print")
	v := fmt.Sprintf("(%d, %d, %d)", version[0], version[1], version[2])
	data := []struct {
		name string
		want string
	}{
		{
			"ctx-io-read_file-size.star",
			"[//ctx-io-read_file-size.star:6] {\n  \"key\":\n",
		},
		{
			"ctx-io-read_file.star",
			"[//ctx-io-read_file.star:7] {\"key\": \"value\"}\n",
		},
		{
			"ctx-os-exec-false.star",
			"[//ctx-os-exec-false.star:6] retcode: 1\n",
		},
		{
			"ctx-os-exec-success.star",
			"[//ctx-os-exec-success.star:6] retcode: 0\n",
		},
		{
			"ctx-re-allmatches.star",
			"[//ctx-re-allmatches.star:7] ()\n" +
				"[//ctx-re-allmatches.star:9] (match(groups = (\"TODO(foo)\",), offset = 4), match(groups = (\"TODO(bar)\",), offset = 14))\n" +
				"[//ctx-re-allmatches.star:11] (match(groups = (\"anc\", \"n\", \"c\"), offset = 0),)\n",
		},
		{
			"ctx-re-match.star",
			"[//ctx-re-match.star:7] None\n" +
				"[//ctx-re-match.star:9] match(groups = (\"TODO(foo)\",), offset = 4)\n" +
				"[//ctx-re-match.star:11] match(groups = (\"anc\", \"n\", \"c\"), offset = 0)\n" +
				"[//ctx-re-match.star:13] match(groups = (\"a\", None), offset = 0)\n",
		},
		{
			"dir-ctx.star",
			"[//dir-ctx.star:6] [\"emit\", \"io\", \"os\", \"re\", \"scm\"]\n",
		},
		{
			"dir-shac.star",
			"[//dir-shac.star:5] [\"commit_hash\", \"register_check\", \"version\"]\n",
		},
		{
			"print-shac-version.star",
			"[//print-shac-version.star:5] " + v + "\n",
		},
		{
			"shac-register_check.star",
			"[//shac-register_check.star:6] running\n",
		},
	}
	want := make([]string, len(data))
	for i := range data {
		want[i] = data[i].name
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			testStarlarkPrint(t, p, data[i].name, false, data[i].want)
		})
	}
}

// Utilities

// testStarlarkPrint test a starlark file that calls print().
func testStarlarkPrint(t *testing.T, root, name string, all bool, want string) {
	r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
	if err := Run(context.Background(), root, name, all, &r); err != nil {
		t.Helper()
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, r.b.String()); diff != "" {
		t.Helper()
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
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
	initGit(t, root)

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

func initGit(t *testing.T, dir string) {
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "engine test")
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
	writeFileBytes(t, root, path, []byte(content))
}

func writeFileBytes(t *testing.T, root, path string, content []byte) {
	if err := os.WriteFile(filepath.Join(root, path), content, 0o600); err != nil {
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

func (r *reportNoPrint) EmitAnnotation(ctx context.Context, check, level, message, file string, s Span, replacements []string) error {
	r.t.Errorf("unexpected annotation: %s: %s, %q, %s, %# v, %v", check, level, message, file, s, replacements)
	return errors.New("not implemented")
}

func (r *reportNoPrint) Print(ctx context.Context, file string, line int, message string) {
	r.t.Errorf("unexpected print: %s(%d): %s", file, line, message)
}

type reportPrint struct {
	reportNoPrint
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *reportPrint) Print(ctx context.Context, file string, line int, message string) {
	r.mu.Lock()
	fmt.Fprintf(&r.b, "[%s:%d] %s\n", file, line, message)
	r.mu.Unlock()
}

type reportEmit struct {
	reportNoPrint
	mu          sync.Mutex
	annotations []annotation
}

type annotation struct {
	Check        string
	Level        string
	Message      string
	File         string
	Span         Span
	Replacements []string
}

func (r *reportEmit) EmitAnnotation(ctx context.Context, check, level, message, file string, s Span, replacements []string) error {
	r.mu.Lock()
	r.annotations = append(r.annotations, annotation{
		Check:        check,
		Level:        level,
		Message:      message,
		File:         file,
		Span:         s,
		Replacements: replacements,
	})
	r.mu.Unlock()
	return nil
}

func init() {
	// Silence logging.
	log.SetOutput(io.Discard)
}
