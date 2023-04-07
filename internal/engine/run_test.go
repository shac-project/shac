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
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:17] file1.txt\n" +
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
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:17] scm_affected_files.star\n" +
			"1: # Copyright 2023 The Shac Authors. All rights reserved.\n"
		testStarlark(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:17] file1.txt\n" +
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
		testStarlark(t, root, "scm_affected_files.star", false, want)
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
		testStarlark(t, root, "scm_all_files.star", false, want)
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
		testStarlark(t, root, "scm_affected_files.star", false, want)
	})

	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		// Only a binary file is touched, no lines should be considered
		// affected.
		want := "[//scm_affected_files_new_lines.star:19] no new lines\n"
		testStarlark(t, root, "scm_affected_files_new_lines.star", false, want)
	})

	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:19] no new lines\n"
		testStarlark(t, root, "scm_affected_files_new_lines.star", true, want)
	})
}

// TestTestDataFail runs all the files under testdata/fail/.
func TestTestDataFail(t *testing.T) {
	t.Parallel()
	p, got := enumDir(t, "fail")
	fail, err := filepath.Abs(filepath.Join("testdata", "fail"))
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
			"inner",
			`  //backtrace.star:11:4: in <toplevel>` + "\n" +
				`  //backtrace.star:9:6: in fn1` + "\n" +
				`  //backtrace.star:6:7: in fn2` + "\n" +
				`  <builtin>: in fail` + "\n" +
				`Error: inner`,
		},
		{
			"ctx-io-read_file-abs.star",
			"do not use absolute path",
			"  //ctx-io-read_file-abs.star:6:19: in cb\n" +
				"Error in read_file: do not use absolute path",
		},
		{
			"ctx-io-read_file-dir.star",
			func() string {
				// TODO(maruel): This error comes from the OS, thus this is a very
				// brittle test case.
				if runtime.GOOS == "windows" {
					return "read " + fail + ": Incorrect function."
				}
				return "read " + fail + ": is a directory"
			}(),
			func() string {
				// TODO(maruel): This error comes from the OS, thus this is a very
				// brittle test case.
				prefix := "  //ctx-io-read_file-dir.star:6:19: in cb\nError in read_file: "
				if runtime.GOOS == "windows" {
					return prefix + "read " + fail + ": Incorrect function."
				}
				return prefix + "read " + fail + ": is a directory"
			}(),
		},
		{
			"ctx-io-read_file-escape.star",
			"cannot escape root",
			"  //ctx-io-read_file-escape.star:6:19: in cb\n" +
				"Error in read_file: cannot escape root",
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
					return "open " + inexistant + ": The system cannot find the file specified."
				}
				return "open " + inexistant + ": no such file or directory"
			}(),
			func() string {
				// Work around the fact that path are not yet correctly handled on
				// Windows.
				inexistant := fail + "/inexistant"
				prefix := "  //ctx-io-read_file-inexistant.star:6:19: in cb\nError in read_file: "
				// TODO(maruel): This error comes from the OS, thus this is a very
				// brittle test case.
				if runtime.GOOS == "windows" {
					return prefix + "open " + inexistant + ": The system cannot find the file specified."
				}
				return prefix + "open " + inexistant + ": no such file or directory"
			}(),
		},
		{
			"ctx-io-read_file-missing_arg.star",
			"read_file: missing argument for path",
			"  //ctx-io-read_file-missing_arg.star:6:19: in cb\n" +
				"Error in read_file: read_file: missing argument for path",
		},
		{
			"ctx-io-read_file-size_big.star",
			"invalid size",
			"  //ctx-io-read_file-size_big.star:6:19: in cb\n" +
				"Error in read_file: invalid size",
		},
		{
			"ctx-io-read_file-size_type.star",
			"read_file: for parameter \"size\": got string, want int",
			"  //ctx-io-read_file-size_type.star:6:19: in cb\n" +
				"Error in read_file: read_file: for parameter \"size\": got string, want int",
		},
		{
			"ctx-io-read_file-unclean.star",
			"pass cleaned path",
			"  //ctx-io-read_file-unclean.star:6:19: in cb\n" +
				"Error in read_file: pass cleaned path",
		},
		{
			"ctx-io-read_file-windows.star",
			"use POSIX style path",
			"  //ctx-io-read_file-windows.star:6:19: in cb\n" +
				"Error in read_file: use POSIX style path",
		},
		{
			"ctx-os-exec-bad_arg.star",
			"exec: unexpected keyword argument \"unknown\"",
			"  //ctx-os-exec-bad_arg.star:6:14: in cb\n" +
				"Error in exec: exec: unexpected keyword argument \"unknown\"",
		},
		{
			"ctx-os-exec-bad_type_in_args.star",
			"command args must be strings",
			"  //ctx-os-exec-bad_type_in_args.star:6:14: in cb\n" +
				"Error in exec: command args must be strings",
		},
		{
			"ctx-os-exec-command_not_in_path.star",
			func() string {
				if runtime.GOOS == "windows" {
					return `exec: "this-command-does-not-exist": executable file not found in %PATH%`
				}
				return `exec: "this-command-does-not-exist": executable file not found in $PATH`
			}(),
			func() string {
				prefix := "  //ctx-os-exec-command_not_in_path.star:6:14: in cb\nError in exec: "
				if runtime.GOOS == "windows" {
					return prefix + `exec: "this-command-does-not-exist": executable file not found in %PATH%`
				}
				return prefix + `exec: "this-command-does-not-exist": executable file not found in $PATH`
			}(),
		},
		{
			"ctx-os-exec-invalid_cwd.star",
			"cannot escape root",
			"  //ctx-os-exec-invalid_cwd.star:6:14: in cb\n" +
				"Error in exec: cannot escape root",
		},
		{
			"ctx-os-exec-no_cmd.star",
			"cmdline must not be an empty list",
			"  //ctx-os-exec-no_cmd.star:6:14: in cb\n" +
				"Error in exec: cmdline must not be an empty list",
		},
		{
			"ctx-re-allmatches-no_arg.star",
			"allmatches: missing argument for pattern",
			"  //ctx-re-allmatches-no_arg.star:6:20: in cb" +
				"\nError in allmatches: allmatches: missing argument for pattern",
		},
		{
			"ctx-re-match-bad_re.star",
			"error parsing regexp: missing closing ): `(`",
			"  //ctx-re-match-bad_re.star:6:15: in cb\n" +
				"Error in match: error parsing regexp: missing closing ): `(`",
		},
		{
			"ctx-re-match-no_arg.star",
			"match: missing argument for pattern",
			"  //ctx-re-match-no_arg.star:6:15: in cb\n" +
				"Error in match: match: missing argument for pattern",
		},
		{
			"ctx-scm-affected_files-arg.star",
			"affected_files: got 1 arguments, want at most 0",
			"  //ctx-scm-affected_files-arg.star:6:25: in cb\n" +
				"Error in affected_files: affected_files: got 1 arguments, want at most 0",
		},
		{
			"ctx-scm-affected_files-kwarg.star",
			"affected_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-affected_files-kwarg.star:6:25: in cb\n" +
				"Error in affected_files: affected_files: unexpected keyword argument \"unexpected\"",
		},
		{
			"ctx-scm-all_files-arg.star",
			"all_files: got 1 arguments, want at most 0",
			"  //ctx-scm-all_files-arg.star:6:20: in cb\n" +
				"Error in all_files: all_files: got 1 arguments, want at most 0",
		},
		{
			"ctx-scm-all_files-kwarg.star",
			"all_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-all_files-kwarg.star:6:20: in cb\n" +
				"Error in all_files: all_files: unexpected keyword argument \"unexpected\"",
		},
		{
			"empty.star",
			"did you forget to call shac.register_check?",
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
			"shac-register_check-kwarg.star",
			"register_check: unexpected keyword argument \"callback\"",
			`  //shac-register_check-kwarg.star:8:20: in <toplevel>` + "\n" +
				`Error in register_check: register_check: unexpected keyword argument "callback"`,
		},
		{
			"shac-register_check-no_arg.star",
			"register_check: missing argument for cb",
			`  //shac-register_check-no_arg.star:5:20: in <toplevel>` + "\n" +
				`Error in register_check: register_check: missing argument for cb`,
		},
		{
			"shac-register_check-recursive.star",
			"can't register checks after done loading",
			"  //shac-register_check-recursive.star:9:22: in cb1\n" +
				"Error in register_check: can't register checks after done loading",
		},
		{
			"shac-register_check-return.star",
			`check "cb" returned an object of type string, expected None`,
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
			err := Run(context.Background(), p, data[i].name, false, &reportNoPrint{t: t})
			if err == nil {
				t.Fatal("expecting an error")
			}
			if diff := cmp.Diff(data[i].err, err.Error()); diff != "" {
				t.Fatalf("mismatch (+want -got):\n%s", diff)
			}
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
			`[//ctx-re-allmatches.star:7] ()` + "\n" +
				`[//ctx-re-allmatches.star:9] (match(groups = ("TODO(foo)",), offset = 4), match(groups = ("TODO(bar)",), offset = 14))` + "\n" +
				`[//ctx-re-allmatches.star:11] (match(groups = ("anc", "n", "c"), offset = 0),)` + "\n",
		},
		{
			"ctx-re-match.star",
			`[//ctx-re-match.star:7] None` + "\n" +
				`[//ctx-re-match.star:9] match(groups = ("TODO(foo)",), offset = 4)` + "\n" +
				`[//ctx-re-match.star:11] match(groups = ("anc", "n", "c"), offset = 0)` + "\n" +
				`[//ctx-re-match.star:13] match(groups = ("a", None), offset = 0)` + "\n",
		},
		{
			"dir-ctx.star",
			"[//dir-ctx.star:6] [\"io\", \"os\", \"re\", \"result\", \"scm\"]\n",
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
	if err := Run(context.Background(), root, name, all, &r); err != nil {
		t.Helper()
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, r.b.String()); diff != "" {
		t.Helper()
		t.Fatalf("mismatch (+want -got):\n%s", diff)
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
