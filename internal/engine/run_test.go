// Copyright 2023 The Shac Authors
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/shac-project/shac/internal/nsjail"
)

func TestRun_Fail(t *testing.T) {
	t.Parallel()
	data := []struct {
		o   Options
		err string
	}{
		{
			Options{
				Config: "/dev/null",
			},
			"no such module",
		},
		{
			Options{
				Config: ".",
			},
			func() string {
				if runtime.GOOS == "windows" {
					return "...Incorrect function."
				}
				return "... is a directory"
			}(),
		},
		{
			Options{
				Main: func() string {
					if runtime.GOOS == "windows" {
						return "c:\\invalid"
					}
					return "/dev/null"
				}(),
			},
			"main file must not be an absolute path",
		},
		{
			Options{
				Config: "testdata/config/min_shac_version-high.textproto",
			},
			"unsupported min_shac_version \"1000\", running 0.0.1",
		},
		{
			Options{
				Config: "testdata/config/min_shac_version-long.textproto",
			},
			"invalid min_shac_version",
		},
		{
			Options{
				Config: "testdata/config/min_shac_version-str.textproto",
			},
			"invalid min_shac_version",
		},
		{
			Options{
				Config: "testdata/config/syntax.textproto",
			},
			// The encoding is not deterministic.
			"...: unknown field: bad",
		},
	}
	for i := range data {
		i := i
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			o := data[i].o
			o.Report = &reportNoPrint{t: t}
			err := Run(context.Background(), &o)
			if err == nil {
				t.Fatal("expecting an error")
			}
			s := err.Error()
			if strings.HasPrefix(data[i].err, "...") {
				if !strings.HasSuffix(s, data[i].err[3:]) {
					t.Fatal(err)
				}
			} else if diff := cmp.Diff(data[i].err, s); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRun_SCM_Raw(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "file1.txt", "First file")
	copySCM(t, root)
	t.Run("affected", func(t *testing.T) {
		want := "[//scm_affected_files.star:19] \n" +
			"file1.txt: \n" +
			"scm_affected_files.star: \n" +
			"scm_affected_files_new_lines.star: \n" +
			"scm_all_files.star: \n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:31] file1.txt\n" +
			"1: First file\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:19] \n" +
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
		want := "[//scm_affected_files.star:19] \n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files.star:19] \n" +
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
		want := "[//scm_all_files.star:19] \n" +
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
		want := "[//scm_affected_files.star:19] \n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:31] scm_affected_files.star\n" +
			"1: # Copyright 2023 The Shac Authors\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:31] file1.txt\n" +
			"1: First file\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", true, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:19] \n" +
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
		want := "[//scm_affected_files.star:19] \n" +
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
		want := "[//scm_all_files.star:19] \n" +
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
		want := "[//scm_affected_files.star:19] \n" +
			".gitmodules: A\n" +
			"scm_affected_files.star: A\n" +
			"scm_affected_files_new_lines.star: A\n" +
			"scm_all_files.star: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})

	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:19] \n" +
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

func TestRun_SCM_DeletedFile(t *testing.T) {
	t.Parallel()

	root := makeGit(t)
	copySCM(t, root)

	writeFile(t, root, "file-to-delete.txt", "This file will be deleted")
	runGit(t, root, "add", "file-to-delete.txt")
	runGit(t, root, "commit", "-m", "Add file-to-delete.txt")

	runGit(t, root, "rm", "file-to-delete.txt")

	t.Run("affected", func(t *testing.T) {
		want := "[//scm_affected_files.star:19] \n\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})
	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:22] no affected files\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_all_files.star:19] \n" +
			"file1.txt: A\n" +
			"file2.txt: A\n" +
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
		want := "[//scm_affected_files.star:19] \n" +
			"a.bin: A\n" +
			"\n"
		testStarlarkPrint(t, root, "scm_affected_files.star", false, want)
	})

	t.Run("affected_new_lines", func(t *testing.T) {
		t.Parallel()
		// Only a binary file is touched, no lines should be considered
		// affected.
		want := "[//scm_affected_files_new_lines.star:33] no new lines\n"
		testStarlarkPrint(t, root, "scm_affected_files_new_lines.star", false, want)
	})

	t.Run("affected_new_lines/all", func(t *testing.T) {
		t.Parallel()
		want := "[//scm_affected_files_new_lines.star:33] no new lines\n"
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
	o := Options{Report: &reportNoPrint{t: t}, Root: root, Main: "scm_affected_files.star"}
	if err = Run(context.Background(), &o); err == nil {
		t.Fatal("expected error")
	}
	want := "error running git --no-optional-locks rev-parse --show-toplevel: exit status 128\nfatal: invalid gitfile format: " + dotGit + "\n"
	if diff := cmp.Diff(want, err.Error()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestRun_SCM_Git_Recursive(t *testing.T) {
	t.Parallel()
	// Tree content:
	//   shac.star
	//   a/
	//     shac.star
	//     a.txt  (not affected)
	//   b/
	//     shac.star
	//     b.txt
	root := t.TempDir()
	initGit(t, root)
	for _, p := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(root, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// a/a.txt is in the initial commit, thus is not affected in commit HEAD.
	writeFile(t, root, "a/a.txt", "content a")
	runGit(t, root, "add", "a/a.txt")
	runGit(t, root, "commit", "-m", "Initial commit")
	// The affected files:
	writeFile(t, root, "shac.star", ""+
		"def cb(ctx):\n"+
		"  name = \"root\"\n"+
		"  for p, m in ctx.scm.affected_files().items():\n"+
		"    if p.endswith(\".txt\"):\n"+
		"      print(name + \": \" + p + \"=\" + m.new_lines()[0][1])\n"+
		"      ctx.emit.annotation(level=\"notice\", message=name, filepath=p)\n"+
		"    else:\n"+
		"      print(name + \": \" + p)\n"+
		"shac.register_check(cb)\n")
	writeFile(t, root, "a/shac.star", ""+
		"def cb(ctx):\n"+
		"  name = \"a\"\n"+
		"  for p, m in ctx.scm.affected_files().items():\n"+
		"    if p.endswith(\".txt\"):\n"+
		"      print(name + \": \" + p + \"=\" + m.new_lines()[0][1])\n"+
		"      ctx.emit.annotation(level=\"notice\", message=name, filepath=p)\n"+
		"    else:\n"+
		"      print(name + \": \" + p)\n"+
		"shac.register_check(cb)\n")
	writeFile(t, root, "b/b.txt", "content b")
	writeFile(t, root, "b/shac.star", ""+
		"def cb(ctx):\n"+
		"  name = \"b\"\n"+
		"  for p, m in ctx.scm.affected_files().items():\n"+
		"    if p.endswith(\".txt\"):\n"+
		"      print(name + \": \" + p + \"=\" + m.new_lines()[0][1])\n"+
		"      ctx.emit.annotation(level=\"notice\", message=name, filepath=p)\n"+
		"    else:\n"+
		"      print(name + \": \" + p)\n"+
		"shac.register_check(cb)\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Second commit")
	r := reportEmitPrint{reportPrint: reportPrint{reportNoPrint: reportNoPrint{t: t}}}
	o := Options{Report: &r, Root: root, Recurse: true}
	if err := Run(context.Background(), &o); err != nil {
		t.Fatal(err)
	}
	// a/a.txt is skipped because it was in the first commit.
	// shac.star see all files.
	// a/shac.star only see files in a/.
	// b/shac.star only see files in b/.
	want := "\n" +
		"[//shac.star:5] b: b.txt=content b\n" +
		"[//shac.star:5] root: b/b.txt=content b\n" +
		"[//shac.star:8] a: shac.star\n" +
		"[//shac.star:8] b: shac.star\n" +
		"[//shac.star:8] root: a/shac.star\n" +
		"[//shac.star:8] root: b/shac.star\n" +
		"[//shac.star:8] root: shac.star"
	// With parallel execution, the output will not be deterministic. Sort it manually.
	a := strings.Split(r.b.String(), "\n")
	sort.Strings(a)
	got := strings.Join(a, "\n")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	annotations := []annotation{
		{
			Check:   "cb",
			Level:   "notice",
			Message: "b",
			Root:    filepath.Join(root, "b"),
			File:    "b.txt",
		},
		{
			Check:   "cb",
			Level:   "notice",
			Message: "root",
			Root:    root,
			File:    "b/b.txt",
		},
	}
	// With parallel execution, the output will not be deterministic. Sort it manually.
	sort.Slice(r.annotations, func(i, j int) bool { return r.annotations[i].File < r.annotations[j].File })
	if diff := cmp.Diff(annotations, r.annotations); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

// TestTestDataFailOrThrow runs all the files under testdata/fail_or_throw/.
//
// These test cases call fail() or throw an exception.
func TestTestDataFailOrThrow(t *testing.T) {
	t.Parallel()
	// When running on Windows, the git installation may not have added the git
	// environment. This is the case on M-A's personal workstation. In this case,
	// some tests fail. Defaults to true since this is the case on GitHub Actions
	// Windows worker.
	isBashAvail := true
	root, got := enumDir(t, "fail_or_throw")
	data := []struct {
		name  string
		err   string
		trace string
	}{
		{
			"backtrace.star",
			"fail: inner",
			"  //backtrace.star:21:4: in <toplevel>\n" +
				"  //backtrace.star:19:6: in fn1\n" +
				"  //backtrace.star:16:7: in fn2\n",
		},
		{
			"ctx-emit-annotation-col-line.star",
			"ctx.emit.annotation: for parameter \"col\": \"line\" must be specified",
			"  //ctx-emit-annotation-col-line.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-col.star",
			"ctx.emit.annotation: for parameter \"col\": got -10, line are 1 based",
			"  //ctx-emit-annotation-col.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-end_col-col-reverse.star",
			"ctx.emit.annotation: for parameter \"end_col\": must be greater than or equal to \"col\"",
			"  //ctx-emit-annotation-end_col-col-reverse.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-end_col-col.star",
			"ctx.emit.annotation: for parameter \"end_col\": \"col\" must be specified",
			"  //ctx-emit-annotation-end_col-col.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-end_col.star",
			"ctx.emit.annotation: for parameter \"end_col\": got -10, line are 1 based",
			"  //ctx-emit-annotation-end_col.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-end_line-line-reverse.star",
			"ctx.emit.annotation: for parameter \"end_line\": must be greater than or equal to \"line\"",
			"  //ctx-emit-annotation-end_line-line-reverse.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-end_line-line.star",
			"ctx.emit.annotation: for parameter \"end_line\": \"line\" must be specified",
			"  //ctx-emit-annotation-end_line-line.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-end_line.star",
			"ctx.emit.annotation: for parameter \"end_line\": got -10, line are 1 based",
			"  //ctx-emit-annotation-end_line.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-kwarg.star",
			"ctx.emit.annotation: unexpected keyword argument \"foo\"",
			"  //ctx-emit-annotation-kwarg.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-level.star",
			"ctx.emit.annotation: for parameter \"level\": got \"invalid\", want one of \"notice\", \"warning\" or \"error\"",
			"  //ctx-emit-annotation-level.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-line.star",
			"ctx.emit.annotation: for parameter \"line\": got -1, line are 1 based",
			"  //ctx-emit-annotation-line.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-message.star",
			"ctx.emit.annotation: for parameter \"message\": got \"\", want string",
			"  //ctx-emit-annotation-message.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-replacements-end_line.star",
			"ctx.emit.annotation: for parameter \"replacements\": \"end_line\" must be specified",
			"  //ctx-emit-annotation-replacements-end_line.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-replacements-list.star",
			"ctx.emit.annotation: for parameter \"replacements\": got list, want sequence of str",
			"  //ctx-emit-annotation-replacements-list.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-replacements-str.star",
			"ctx.emit.annotation: for parameter \"replacements\": got string, want starlark.Sequence",
			"  //ctx-emit-annotation-replacements-str.star:16:22: in cb\n",
		},
		{
			"ctx-emit-annotation-replacements-tuple.star",
			"ctx.emit.annotation: for parameter \"replacements\": got tuple, want sequence of str",
			"  //ctx-emit-annotation-replacements-tuple.star:16:22: in cb\n",
		},
		{
			"ctx-emit-artifact-dir.star",
			"ctx.emit.artifact: for parameter \"filepath\": \".\" is a directory",
			"  //ctx-emit-artifact-dir.star:16:20: in cb\n",
		},
		{
			"ctx-emit-artifact-inexistant.star",
			"ctx.emit.artifact: for parameter \"filepath\": \"inexistant\" not found",
			"  //ctx-emit-artifact-inexistant.star:16:20: in cb\n",
		},
		{
			"ctx-emit-artifact-kwarg.star",
			"ctx.emit.artifact: unexpected keyword argument \"foo\"",
			"  //ctx-emit-artifact-kwarg.star:16:20: in cb\n",
		},
		{
			"ctx-emit-artifact-type.star",
			"ctx.emit.artifact: for parameter \"content\": got int, want str or bytes",
			"  //ctx-emit-artifact-type.star:16:20: in cb\n",
		},
		{
			"ctx-emit-artifact-windows.star",
			"ctx.emit.artifact: for parameter \"filepath\": \"foo\\\\bar\" use POSIX style path",
			"  //ctx-emit-artifact-windows.star:16:20: in cb\n",
		},
		{
			"ctx-immutable.star",
			"can't assign to .key field of struct",
			"  //ctx-immutable.star:17:6: in cb\n",
		},
		{
			"ctx-io-read_file-abs.star",
			"ctx.io.read_file: for parameter \"filepath\": \"/dev/null\" do not use absolute path",
			"  //ctx-io-read_file-abs.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-dir.star",
			"ctx.io.read_file: for parameter \"filepath\": \".\" is a directory",
			"  //ctx-io-read_file-dir.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-escape.star",
			"ctx.io.read_file: for parameter \"filepath\": \"../checks.go\" cannot escape root",
			"  //ctx-io-read_file-escape.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-inexistant.star",
			"ctx.io.read_file: for parameter \"filepath\": \"inexistant\" not found",
			"  //ctx-io-read_file-inexistant.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-missing_arg.star",
			"ctx.io.read_file: missing argument for filepath",
			"  //ctx-io-read_file-missing_arg.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-size_big.star",
			"ctx.io.read_file: for parameter \"size\": 36893488147419103232 is an invalid size",
			"  //ctx-io-read_file-size_big.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-size_type.star",
			"ctx.io.read_file: for parameter \"size\": got string, want int",
			"  //ctx-io-read_file-size_type.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-unclean.star",
			"ctx.io.read_file: for parameter \"filepath\": \"path/../file.txt\" pass cleaned path",
			"  //ctx-io-read_file-unclean.star:16:19: in cb\n",
		},
		{
			"ctx-io-read_file-windows.star",
			"ctx.io.read_file: for parameter \"filepath\": \"test\\\\data.txt\" use POSIX style path",
			"  //ctx-io-read_file-windows.star:16:19: in cb\n",
		},
		{
			"ctx-os-exec-bad_arg.star",
			"ctx.os.exec: unexpected keyword argument \"unknown\"",
			"  //ctx-os-exec-bad_arg.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-bad_env_key.star",
			"ctx.os.exec: \"env\" key is not a string: 1",
			"  //ctx-os-exec-bad_env_key.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-bad_env_value.star",
			"ctx.os.exec: \"env\" value is not a string: 1",
			"  //ctx-os-exec-bad_env_value.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-bad_type_in_args.star",
			"ctx.os.exec: for parameter \"cmd\": got list, want sequence of str",
			"  //ctx-os-exec-bad_type_in_args.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-command_not_in_path.star",
			func() string {
				if runtime.GOOS == "windows" {
					return "ctx.os.exec: exec: \"this-command-does-not-exist\": executable file not found in %PATH%"
				}
				return "ctx.os.exec: exec: \"this-command-does-not-exist\": executable file not found in $PATH"
			}(),
			"  //ctx-os-exec-command_not_in_path.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-false.star",
			func() string {
				if !isBashAvail && runtime.GOOS == "windows" {
					return "ctx.os.exec: exec: \"false\": executable file not found in %PATH%"
				}
				return "ctx.os.exec: command failed with exit code 1: [\"false\"]"
			}(),
			"  //ctx-os-exec-false.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-invalid_cwd.star",
			"ctx.os.exec: cannot escape root",
			"  //ctx-os-exec-invalid_cwd.star:16:14: in cb\n",
		},
		{
			"ctx-os-exec-mutate_result.star",
			func() string {
				if !isBashAvail && runtime.GOOS == "windows" {
					return "ctx.os.exec: exec: \"echo\": executable file not found in %PATH%"
				}
				return "can't assign to .retcode field of struct"
			}(),
			func() string {
				if !isBashAvail && runtime.GOOS == "windows" {
					return "  //ctx-os-exec-mutate_result.star:16:20: in cb\n"
				}
				return "  //ctx-os-exec-mutate_result.star:17:6: in cb\n"
			}(),
		},
		{
			"ctx-os-exec-no_cmd.star",
			"ctx.os.exec: cmdline must not be an empty list",
			"  //ctx-os-exec-no_cmd.star:16:14: in cb\n",
		},
		{
			"ctx-re-allmatches-no_arg.star",
			"ctx.re.allmatches: missing argument for pattern",
			"  //ctx-re-allmatches-no_arg.star:16:20: in cb\n",
		},
		{
			"ctx-re-match-bad_re.star",
			"ctx.re.match: error parsing regexp: missing closing ): `(`",
			"  //ctx-re-match-bad_re.star:16:15: in cb\n",
		},
		{
			"ctx-re-match-no_arg.star",
			"ctx.re.match: missing argument for pattern",
			"  //ctx-re-match-no_arg.star:16:15: in cb\n",
		},
		{
			"ctx-scm-affected_files-arg.star",
			"ctx.scm.affected_files: got 1 arguments, want at most 0",
			"  //ctx-scm-affected_files-arg.star:16:25: in cb\n",
		},
		{
			"ctx-scm-affected_files-kwarg.star",
			"ctx.scm.affected_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-affected_files-kwarg.star:16:25: in cb\n",
		},
		{
			"ctx-scm-all_files-arg.star",
			"ctx.scm.all_files: got 1 arguments, want at most 0",
			"  //ctx-scm-all_files-arg.star:16:20: in cb\n",
		},
		{
			"ctx-scm-all_files-kwarg.star",
			"ctx.scm.all_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-all_files-kwarg.star:16:20: in cb\n",
		},
		{
			"empty.star",
			"did you forget to call shac.register_check?",
			"",
		},
		{
			"fail-check.star",
			"fail: an  unexpected  failure  None\nfail: unexpected keyword argument \"unknown\"",
			"  //fail-check.star:16:7: in cb\n",
		},
		{
			"fail.star",
			"fail: an expected failure",
			"  //fail.star:15:5: in <toplevel>\n",
		},
		{
			"shac-immutable.star",
			"can't assign to .key field of struct",
			"  //shac-immutable.star:16:5: in <toplevel>\n",
		},
		{
			"shac-register_check-builtin.star",
			"shac.register_check: callback must be a function accepting one \"ctx\" argument",
			"  //shac-register_check-builtin.star:15:20: in <toplevel>\n",
		},
		{
			"shac-register_check-callback.star",
			"shac.register_check: callback must be a function accepting one \"ctx\" argument",
			"  //shac-register_check-callback.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-kwarg.star",
			"shac.register_check: unexpected keyword argument \"invalid\"",
			"  //shac-register_check-kwarg.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-lambda.star",
			"shac.register_check: \"name\" must be set when callback is a lambda",
			"  //shac-register_check-lambda.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-no_arg.star",
			"shac.register_check: missing argument for callback",
			"  //shac-register_check-no_arg.star:15:20: in <toplevel>\n",
		},
		{
			"shac-register_check-recursive.star",
			"shac.register_check: can't register checks after done loading",
			"  //shac-register_check-recursive.star:19:22: in cb1\n",
		},
		{
			"shac-register_check-return.star",
			"check \"cb\" returned an object of type string, expected None",
			"",
		},
		{
			"shac-register_check-twice.star",
			"shac.register_check: can't register two checks with the same name \"cb\"",
			"  //shac-register_check-twice.star:22:20: in <toplevel>\n",
		},
		{
			"syntax_error.star",
			"//syntax_error.star:15:3: got '//', want primary expression",
			"",
		},
		{
			"undefined_symbol.star",
			"//undefined_symbol.star:15:1: undefined: undefined_symbol",
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
			o := Options{Report: &reportNoPrint{t: t}, Root: root, Main: data[i].name}
			err := Run(context.Background(), &o)
			if err == nil {
				t.Fatal("expecting an error")
			}
			if diff := cmp.Diff(data[i].err, err.Error()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
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
					t.Errorf("mismatch (-want +got):\n%s", diff)
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
		artifacts   []artifact
		err         string
	}{
		{
			"ctx-emit-annotation-error.star",
			[]annotation{
				{
					Check:        "cb",
					Level:        Error,
					Message:      "bad code",
					Root:         root,
					File:         "file.txt",
					Span:         Span{Start: Cursor{Line: 1, Col: 1}, End: Cursor{Line: 10, Col: 1}},
					Replacements: []string{"nothing", "broken code"},
				},
			},
			nil,
			"a check failed",
		},
		{
			"ctx-emit-annotation.star",
			[]annotation{
				{
					Check:        "cb",
					Level:        Warning,
					Message:      "please fix",
					Root:         root,
					File:         "file.txt",
					Span:         Span{Start: Cursor{Line: 1, Col: 1}, End: Cursor{Line: 10, Col: 1}},
					Replacements: []string{"a", "tuple"},
				},
				{
					Check:   "cb",
					Level:   Notice,
					Message: "great code",
					Span:    Span{Start: Cursor{Line: 100, Col: 2}},
				},
				{
					Check:        "cb",
					Level:        Warning,
					Message:      "please fix",
					Root:         root,
					File:         "file.txt",
					Span:         Span{Start: Cursor{Line: 1, Col: 1}, End: Cursor{Line: 10, Col: 1}},
					Replacements: []string{"a", "list"},
				},
				{
					Check:        "cb",
					Level:        "warning",
					Message:      "weird",
					Span:         Span{Start: Cursor{Line: 1}, End: Cursor{Line: 10}},
					Replacements: []string{"a", "dict"},
				},
			},
			nil,
			"",
		},
		{
			"ctx-emit-artifact.star",
			nil,
			[]artifact{
				{
					Check:   "cb",
					File:    "file.txt",
					Content: []byte("content as str"),
				},
				{
					Check:   "cb",
					File:    "file.txt",
					Content: []byte("content as bytes"),
				},
				{
					Check: "cb",
					Root:  root,
					File:  "file.txt",
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
			r := reportEmitNoPrint{reportNoPrint: reportNoPrint{t: t}}
			o := Options{Report: &r, Root: root, Main: data[i].name, Config: "../config/valid.textproto"}
			err := Run(context.Background(), &o)
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
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(data[i].artifacts, r.artifacts); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
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
		name        string
		want        string
		skipWindows bool
	}{
		{
			name: "ctx-io-read_file-size.star",
			want: "[//ctx-io-read_file-size.star:16] {\n  \"key\":\n",
		},
		{
			name: "ctx-io-read_file.star",
			want: "[//ctx-io-read_file.star:17] {\"key\": \"value\"}\n",
		},
		{
			name: "ctx-os-exec-env.star",
			want: "[//ctx-os-exec-env.star:20] FOO=foo-value\nBAR=bar-value\n",
			// TODO(olivernewman): Make this test support Windows by running a
			// batch file instead of a shell script.
			skipWindows: true,
		},
		{
			name: "ctx-os-exec-success.star",
			want: "[//ctx-os-exec-success.star:17] retcode: 0\n" +
				"[//ctx-os-exec-success.star:18] stdout: hello from stdout\n" +
				"[//ctx-os-exec-success.star:19] stderr: hello from stderr\n",
			// TODO(olivernewman): Make this test support Windows by running a
			// batch file instead of a shell script.
			skipWindows: true,
		},
		{
			name: "ctx-re-allmatches.star",
			want: "[//ctx-re-allmatches.star:17] ()\n" +
				"[//ctx-re-allmatches.star:19] (match(groups = (\"TODO(foo)\",), offset = 4), match(groups = (\"TODO(bar)\",), offset = 14))\n" +
				"[//ctx-re-allmatches.star:21] (match(groups = (\"anc\", \"n\", \"c\"), offset = 0),)\n",
		},
		{
			name: "ctx-re-match.star",
			want: "[//ctx-re-match.star:17] None\n" +
				"[//ctx-re-match.star:19] match(groups = (\"TODO(foo)\",), offset = 4)\n" +
				"[//ctx-re-match.star:21] match(groups = (\"anc\", \"n\", \"c\"), offset = 0)\n" +
				"[//ctx-re-match.star:23] match(groups = (\"a\", None), offset = 0)\n",
		},
		{
			name: "dir-ctx.star",
			want: "[//dir-ctx.star:16] [\"emit\", \"io\", \"os\", \"re\", \"scm\"]\n",
		},
		{
			name: "dir-shac.star",
			want: "[//dir-shac.star:15] [\"commit_hash\", \"register_check\", \"version\"]\n",
		},
		{
			name: "print-shac-version.star",
			want: "[//print-shac-version.star:15] " + v + "\n",
		},
		{
			name: "shac-register_check.star",
			want: "[//shac-register_check.star:16] running\n",
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
			if runtime.GOOS == "windows" && data[i].skipWindows {
				t.Skip("not supported on windows")
			}
			testStarlarkPrint(t, p, data[i].name, false, data[i].want)
		})
	}
}

func TestRun_Filesystem_Sandboxing(t *testing.T) {
	if len(nsjail.Exec) == 0 {
		t.Skip("sandboxing is only supported on linux-{arm64,amd64}")
	}
	t.Parallel()

	// This file should appear to be nonexistent to commands run within the
	// sandbox.
	fileOutsideRoot := filepath.Join(t.TempDir(), "foo.txt")
	if err := os.WriteFile(fileOutsideRoot, []byte("foo"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()

	writeFile(t, root, "foo.sh", ""+
		"#!/bin/sh\n"+
		"set -e\n"+
		"cat \""+fileOutsideRoot+"\"\n")
	if err := os.Chmod(filepath.Join(root, "foo.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "shac.star", ""+
		"def cb(ctx):\n"+
		"  res = ctx.os.exec([ctx.scm.root + \"/foo.sh\"], raise_on_failure = False)\n"+
		"  print(\"retcode: %d\" % res.retcode)\n"+
		"  print(res.stderr)\n"+
		"shac.register_check(cb)\n")

	want := "[//shac.star:3] retcode: 1\n" +
		"[//shac.star:4] cat: " + fileOutsideRoot + ": No such file or directory\n\n"
	testStarlarkPrint(t, root, "shac.star", false, want)
}

// Utilities

// testStarlarkPrint test a starlark file that calls print().
func testStarlarkPrint(t testing.TB, root, name string, all bool, want string) {
	r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
	o := Options{Report: &r, Root: root, Main: name, AllFiles: all}
	if err := Run(context.Background(), &o); err != nil {
		t.Helper()
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, r.b.String()); diff != "" {
		t.Helper()
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func enumDir(t *testing.T, name string) (string, []string) {
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
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
	t testing.TB
}

func (r *reportNoPrint) EmitAnnotation(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	r.t.Errorf("unexpected annotation: %s: %s, %q, %s, %s, %# v, %v", check, level, message, root, file, s, replacements)
	return errors.New("not implemented")
}

func (r *reportNoPrint) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	r.t.Errorf("unexpected artifact: %s: %s", check, file)
	return errors.New("not implemented")
}

func (r *reportNoPrint) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, l Level, err error) {
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

type annotation struct {
	Check        string
	Level        Level
	Message      string
	Root         string
	File         string
	Span         Span
	Replacements []string
}

type artifact struct {
	Check   string
	Root    string
	File    string
	Content []byte
}

type reportEmitNoPrint struct {
	reportNoPrint
	mu          sync.Mutex
	annotations []annotation
	artifacts   []artifact
}

func (r *reportEmitNoPrint) EmitAnnotation(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	r.mu.Lock()
	r.annotations = append(r.annotations, annotation{
		Check:        check,
		Level:        level,
		Message:      message,
		Root:         root,
		File:         file,
		Span:         s,
		Replacements: replacements,
	})
	r.mu.Unlock()
	return nil
}

func (r *reportEmitNoPrint) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	r.mu.Lock()
	r.artifacts = append(r.artifacts, artifact{Check: check, Root: root, File: file, Content: content})
	r.mu.Unlock()
	return nil
}

type reportEmitPrint struct {
	reportPrint
	annotations []annotation
	artifacts   []artifact
}

func (r *reportEmitPrint) EmitAnnotation(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	r.mu.Lock()
	r.annotations = append(r.annotations, annotation{
		Check:        check,
		Level:        level,
		Message:      message,
		Root:         root,
		File:         file,
		Span:         s,
		Replacements: replacements,
	})
	r.mu.Unlock()
	return nil
}

func (r *reportEmitPrint) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	r.mu.Lock()
	r.artifacts = append(r.artifacts, artifact{Check: check, Root: root, File: file, Content: content})
	r.mu.Unlock()
	return nil
}

func init() {
	// Silence logging.
	log.SetOutput(io.Discard)
}
