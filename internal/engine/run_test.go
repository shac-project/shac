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
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
)

func TestRun_Fail(t *testing.T) {
	t.Parallel()
	scratchDir := t.TempDir()
	data := []struct {
		name string
		o    Options
		err  string
	}{
		{
			"config path is a directory",
			Options{
				config: ".",
			},
			func() string {
				if runtime.GOOS == "windows" {
					return "...Incorrect function."
				}
				return "... is a directory"
			}(),
		},
		{
			"absolute entrypoint path",
			Options{
				EntryPoint: func() string {
					if runtime.GOOS == "windows" {
						return "c:\\invalid"
					}
					return "/dev/null"
				}(),
			},
			"entrypoint file must not be an absolute path",
		},
		{
			"malformed config file",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ``)
				writeFile(t, root, "shac.textproto", "bad")
				return Options{
					Dir: root,
				}
			}(),
			//
			"...unexpected EOF",
		},
		{
			"config file with unknown field",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ``)
				writeFile(t, root, "shac.textproto", "unknown_field: true\n")
				return Options{
					Dir: root,
				}
			}(),
			"...unknown field: unknown_field",
		},
		{
			"config file with newer min_shac_version and unknown field",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ``)
				writeFile(t, root, "shac.textproto",
					"min_shac_version: \"2000\"\n"+
						"unknown_field: true\n")
				return Options{
					Dir: root,
				}
			}(),
			fmt.Sprintf("min_shac_version specifies unsupported version \"2000\", running %s", Version),
		},
		{
			"no shac.star file",
			Options{
				Dir:        scratchDir,
				EntryPoint: "entrypoint.star",
			},
			fmt.Sprintf("no entrypoint.star file in repository root: %s", scratchDir),
		},
		{
			"no shac.star file and invalid vars",
			Options{
				Dir:        scratchDir,
				EntryPoint: "entrypoint.star",
				Vars: map[string]string{
					"this_is_an_invalid_var": "foo",
				},
			},
			// Missing entrypoint file errors should be prioritized over invalid
			// var errors.
			fmt.Sprintf("no entrypoint.star file in repository root: %s", scratchDir),
		},
		{
			"no shac.star files (recursive)",
			Options{
				Dir:        scratchDir,
				Recurse:    true,
				EntryPoint: "entrypoint.star",
			},
			fmt.Sprintf("no entrypoint.star files found in %s", scratchDir),
		},
		{
			"nonexistent directory",
			Options{
				Dir: "!!!this-is-a-file-that-does-not-exist!!!",
			},
			"no such directory: !!!this-is-a-file-that-does-not-exist!!!",
		},
		{
			"not a directory",
			Options{
				Dir: func() string {
					writeFile(t, scratchDir, "foo.txt", "")
					return filepath.Join(scratchDir, "foo.txt")
				}(),
			},
			fmt.Sprintf("not a directory: %s", filepath.Join(scratchDir, "foo.txt")),
		},
		{
			"invalid var",
			Options{
				Vars: map[string]string{
					"unknown_var": "",
				},
			},
			"var not declared in shac.textproto: unknown_var",
		},
		{
			"invalid var with no config file",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ``)
				return Options{
					Dir: root,
					Vars: map[string]string{
						"unknown_var": "",
					},
				}
			}(),
			"var must be declared in a shac.textproto file: unknown_var",
		},
		{
			"invalid allowlist item",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ""+
					"def cb(ctx):\n"+
					"    pass\n"+
					"shac.register_check(cb)")
				return Options{
					Dir: root,
					Filter: CheckFilter{
						AllowList: []string{"does-not-exist"},
					},
				}
			}(),
			"check does not exist: does-not-exist",
		},
		{
			"invalid denylist item",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ""+
					"def cb(ctx):\n"+
					"    pass\n"+
					"shac.register_check(cb)")
				return Options{
					Dir: root,
					Filter: CheckFilter{
						DenyList: []string{"does-not-exist"},
					},
				}
			}(),
			"check does not exist: does-not-exist",
		},
		{
			"allowlisted and denylisted",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ""+
					"def cb(ctx):\n"+
					"    pass\n"+
					"shac.register_check(cb)")
				return Options{
					Dir: root,
					Filter: CheckFilter{
						AllowList: []string{"cb"},
						DenyList:  []string{"cb"},
					},
				}
			}(),
			"checks cannot be both allowed and denied: cb",
		},
		{
			"multiple invalid allowlist items",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ""+
					"def cb(ctx):\n"+
					"    pass\n"+
					"shac.register_check(cb)")
				return Options{
					Dir: root,
					Filter: CheckFilter{
						AllowList: []string{"does-not-exist", "cb", "also-does-not-exist"},
					},
				}
			}(),
			"checks do not exist: also-does-not-exist, does-not-exist",
		},
		{
			"invalid FormatterFiltering",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ""+
					"def cb(ctx):\n"+
					"    pass\n"+
					"shac.register_check(cb)")
				return Options{
					Dir: root,
					Filter: CheckFilter{
						FormatterFiltering: FormatterFiltering(3),
					},
				}
			}(),
			"invalid FormatterFiltering value: 3",
		},
		{
			"all checks filtered out",
			func() Options {
				root := t.TempDir()
				writeFile(t, root, "shac.star", ""+
					"def cb(ctx):\n"+
					"    pass\n"+
					"shac.register_check(cb)")
				return Options{
					Dir: root,
					Filter: CheckFilter{
						FormatterFiltering: OnlyFormatters,
					},
				}
			}(),
			"no checks to run",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
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

func TestRun_DirOverridden(t *testing.T) {
	t.Parallel()

	data := []struct {
		name string
		// The full error message often includes the directory, hence a function
		// so it's easier to construct the values.
		dir  string
		want string
	}{
		{
			"git-aware",
			func() string {
				root := makeGit(t)
				writeFile(t, root, "shac.star", `print("hello")`)

				// If shac is pointed at a subdirectory of a git repo, it should
				// discover and run checks defined anywhere in the repo.
				subdir := filepath.Join(root, "a", "b")
				mkdirAll(t, subdir)
				return subdir
			}(),
			"[//shac.star:1] hello\n",
		},
	}

	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, data[i].dir, "", false, false, data[i].want)
		})
	}
}

func TestRun_SpecificFiles(t *testing.T) {
	// Not parallelized because it calls os.Chdir.

	root := resolvedTempDir(t)

	writeFile(t, root, "shac.textproto", prototext.Format(&Document{
		Ignore: []string{
			// Specifying files on the command line should override ignores.
			"*.py",
		},
	}))

	writeFile(t, root, "python.py", "a python file")
	writeFile(t, root, "rust.rs", "a rust file")

	copySCM(t, root)

	data := []struct {
		name         string
		starlarkFile string
		want         string
		files        []string
		workDir      string
	}{
		{
			name:         "affected files (no files specified)",
			starlarkFile: "ctx-scm-affected_files.star",
			want: "[//ctx-scm-affected_files.star:19] \n" +
				scmStarlarkFiles("") +
				"rust.rs: \n" +
				"shac.textproto: \n" +
				"\n",
			files:   nil,
			workDir: root,
		},
		{
			name:         "affected files (relative path specified)",
			starlarkFile: "ctx-scm-affected_files.star",
			want: "[//ctx-scm-affected_files.star:19] \n" +
				"python.py: \n" +
				"rust.rs: \n" +
				"\n",
			files:   []string{"python.py", "rust.rs"},
			workDir: root,
		},
		{
			name:         "affected files (absolute path specified)",
			starlarkFile: "ctx-scm-affected_files.star",
			want: "[//ctx-scm-affected_files.star:19] \n" +
				"python.py: \n" +
				"rust.rs: \n" +
				"\n",
			files: []string{filepath.Join(root, "python.py"), filepath.Join(root, "rust.rs")},
			// Absolute paths should work even outside the root.
			workDir: t.TempDir(),
		},
		{
			name:         "all files",
			starlarkFile: "ctx-scm-all_files.star",
			want: "[//ctx-scm-all_files.star:19] \n" +
				"python.py: \n" +
				"rust.rs: \n" +
				"\n",
			files:   []string{"python.py", "rust.rs"},
			workDir: root,
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			originalWd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(data[i].workDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(originalWd); err != nil {
					t.Fatal(err)
				}
			}()
			testStarlarkPrint(t, root, data[i].starlarkFile, false, false, data[i].want, data[i].files...)
		})
	}

	// When recursion is enabled and specific files are listed, all shac.star
	// files on disk that could apply to those files should still be loaded even
	// if those shac.star files are not in the listed files.
	t.Run("recursive shac.star files discovered", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()

		files := []string{
			"root.py",
			filepath.Join("dir1", "dir2", "nested.py"),
		}
		for _, f := range files {
			writeFile(t, root, f, "")
		}

		writeFile(t, root, "shac.star",
			`shac.register_check(
				shac.check(
					lambda ctx: print("hi from root"),
					name="root_check",
				)
			)`)
		writeFile(t, root, "dir1/shac.star",
			`shac.register_check(
				shac.check(
					lambda ctx: print("hi from dir1"),
					name="dir1_check",
				)
			)`)
		writeFile(t, root, "dir1/dir2/shac.star",
			`shac.register_check(
				shac.check(
					lambda ctx: print("hi from dir1/dir2"),
					name="dir2_check",
				)
			)`)

		r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
		var absFiles []string
		for _, f := range files {
			absFiles = append(absFiles, filepath.Join(root, f))
		}
		o := Options{Report: &r, Dir: root, Files: absFiles, Recurse: true}

		if err := Run(context.Background(), &o); err != nil {
			t.Fatal(err)
		}

		// The output comes from multiple checks that run in a nondeterministic
		// order, so the ordering of the output lines may vary.
		gotLines := strings.Split(strings.Trim(r.b.String(), "\n"), "\n")
		slices.Sort(gotLines)

		wantLines := []string{
			"[//dir1/dir2/shac.star:3] hi from dir1/dir2",
			"[//dir1/shac.star:3] hi from dir1",
			"[//shac.star:3] hi from root",
		}

		if diff := cmp.Diff(wantLines, gotLines); diff != "" {
			t.Fatalf("mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestRun_SpecificFiles_Fail(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	subdir := filepath.Join(root, "dir")
	mkdirAll(t, subdir)
	dirOutsideRoot := t.TempDir()

	data := []struct {
		name    string
		files   []string
		wantErr string
	}{
		{
			name:    "path outside root",
			files:   []string{filepath.Join(dirOutsideRoot, "outside-root.txt")},
			wantErr: fmt.Sprintf("cannot analyze file outside root: %s", filepath.Join(dirOutsideRoot, "outside-root.txt")),
		},
		{
			name:    "directory",
			files:   []string{subdir},
			wantErr: fmt.Sprintf("is a directory: %s", subdir),
		},
		{
			name:    "nonexistent file",
			files:   []string{filepath.Join(root, "nonexistent.txt")},
			wantErr: fmt.Sprintf("no such file: %s", filepath.Join(root, "nonexistent.txt")),
		},
	}

	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
			o := Options{Report: &r, Dir: root, EntryPoint: "shac.star", Files: data[i].files}
			err := Run(context.Background(), &o)
			if err == nil {
				t.Fatalf("Expected error: %q", data[i].wantErr)
			} else if err.Error() != data[i].wantErr {
				t.Fatalf("Expected error %q, got %q", data[i].wantErr, err)
			}
		})
	}
}

func TestRun_Filtering(t *testing.T) {
	t.Parallel()

	root := resolvedTempDir(t)

	writeFile(t, root, "shac.star", ""+
		"def non_formatter(ctx):\n"+
		"    print(\"non-formatter running\")\n"+
		"def formatter(ctx):\n"+
		"    print(\"formatter running\")\n"+
		"shac.register_check(shac.check(formatter, formatter = True))\n"+
		"shac.register_check(shac.check(non_formatter))\n")

	data := []struct {
		name   string
		filter CheckFilter
		want   string
	}{
		{
			name: "all checks",
			want: "[//shac.star:2] non-formatter running\n" +
				"[//shac.star:4] formatter running\n",
		},
		{
			name: "allowlist",
			filter: CheckFilter{
				AllowList: []string{"formatter"},
			},
			want: "[//shac.star:4] formatter running\n",
		},
		{
			name: "denylist",
			filter: CheckFilter{
				DenyList: []string{"formatter"},
			},
			want: "[//shac.star:2] non-formatter running\n",
		},

		{
			name: "only formatters",
			filter: CheckFilter{
				FormatterFiltering: OnlyFormatters,
			},
			want: "[//shac.star:4] formatter running\n",
		},
		{
			name: "only non-formatters",
			filter: CheckFilter{
				FormatterFiltering: OnlyNonFormatters,
			},
			want: "[//shac.star:2] non-formatter running\n",
		},
		{
			name: "only specified checks (non-formatter)",
			filter: CheckFilter{
				AllowList: []string{"non_formatter"},
			},
			want: "[//shac.star:2] non-formatter running\n",
		},
		{
			name: "only specified checks (formatter)",
			filter: CheckFilter{
				AllowList: []string{"formatter"},
			},
			want: "[//shac.star:4] formatter running\n",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
			o := Options{Report: &r, Dir: root, Filter: data[i].filter}
			if err := Run(context.Background(), &o); err != nil {
				t.Helper()
				t.Fatal(err)
			}
			got := sortLines(r.b.String())
			want := sortLines(data[i].want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Helper()
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRun_Ignore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "shac.textproto", prototext.Format(&Document{
		Ignore: []string{
			"/dir/",
			"file_in_root.txt",
			"!file_in_root.txt", // Should take precedence.
			"*.ignored",
			"*.star",
		},
	}))

	writeFile(t, root, "file_in_root.txt", "foo")
	writeFile(t, root, "other_file_in_root.txt", "foo")
	writeFile(t, root, "dir/ignored_file.txt", "bar")
	writeFile(t, root, "nested/dir/unignored_file.txt", "baz")
	writeFile(t, root, "other/nested/file.txt", "quux")
	writeFile(t, root, "abc.ignored", "abc")
	writeFile(t, root, "x/def.ignored", "def")

	copySCM(t, root)

	data := []struct {
		name string
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			"[//ctx-scm-affected_files.star:19] \n" +
				"file_in_root.txt: \n" +
				"nested/dir/unignored_file.txt: \n" +
				"other/nested/file.txt: \n" +
				"other_file_in_root.txt: \n" +
				"shac.textproto: \n" +
				"\n",
		},
		{
			"ctx-scm-affected_files-new_lines.star",
			"[//ctx-scm-affected_files-new_lines.star:33] file_in_root.txt\n" +
				"1: foo\n",
		},
		{
			"ctx-scm-all_files.star",
			"[//ctx-scm-all_files.star:19] \n" +
				"file_in_root.txt: \n" +
				"nested/dir/unignored_file.txt: \n" +
				"other/nested/file.txt: \n" +
				"other_file_in_root.txt: \n" +
				"shac.textproto: \n" +
				"\n",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, false, false, data[i].want)
		})
	}

	t.Run("empty ignore field", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "shac.textproto", prototext.Format(&Document{
			Ignore: []string{
				"*.foo",
				"",
			},
		}))

		r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
		o := Options{Report: &r, Dir: root, AllFiles: false, EntryPoint: "shac.star"}
		err := Run(context.Background(), &o)
		if err == nil {
			t.Fatal("Expected empty ignore field to be rejected")
		} else if !errors.Is(err, errEmptyIgnore) {
			t.Fatalf("Expected error %q, got %q", errEmptyIgnore, err)
		}
	})
}

func TestRun_Vars(t *testing.T) {
	t.Parallel()

	data := []struct {
		name       string
		configVars map[string]string
		flagVars   map[string]string
		want       string
	}{
		{
			name:       "default",
			configVars: map[string]string{"foo": "default_foo"},
			want:       "default_foo",
		},
		{
			name:       "overridden",
			configVars: map[string]string{"foo": "default_foo"},
			flagVars:   map[string]string{"foo": "overridden_foo"},
			want:       "overridden_foo",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			main := "ctx-var-value.star"
			copyFile(t, root, filepath.Join("testdata", main))
			r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
			o := Options{
				Report:     &r,
				Dir:        root,
				Vars:       data[i].flagVars,
				EntryPoint: main,
			}

			config := &Document{}
			for name, def := range data[i].configVars {
				config.Vars = append(config.Vars, &Var{
					Name:        name,
					Description: "a variable",
					Default:     def,
				})
			}
			b, err := prototext.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			writeFileBytes(t, root, "shac.textproto", b, 0o600)

			if err = Run(context.Background(), &o); err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("[//ctx-var-value.star:16] %s\n", data[i].want)
			if diff := cmp.Diff(want, r.b.String()); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRun_PassthroughEnv(t *testing.T) {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(t.Name())))

	varPrefix := "TEST_" + hash + "_"
	nonFileVarname := varPrefix + "VAR"
	readOnlyDirVarname := varPrefix + "RO_DIR"
	writeableDirVarname := varPrefix + "WRITEABLE_DIR"
	env := map[string]string{
		nonFileVarname:      "this is not a file",
		readOnlyDirVarname:  filepath.Join(t.TempDir(), "readonly"),
		writeableDirVarname: filepath.Join(t.TempDir(), "writeable"),
	}
	mkdirAll(t, env[readOnlyDirVarname])
	mkdirAll(t, env[writeableDirVarname])

	for k, v := range env {
		t.Setenv(k, v)
	}

	config := &Document{
		PassthroughEnv: []*PassthroughEnv{
			{
				Name: nonFileVarname,
			},
			{
				Name:   readOnlyDirVarname,
				IsPath: true,
			},
			{
				Name:      writeableDirVarname,
				IsPath:    true,
				Writeable: true,
			},
			// Additionally give access to HOME (or LocalAppData on Windows),
			// which contains the Go cache, so checks that run `go run` can use
			// cached artifacts.
			{
				Name:      "HOME",
				IsPath:    true,
				Writeable: true,
			},
			{
				Name:      "LocalAppData",
				IsPath:    true,
				Writeable: true,
			},
		},
		Vars: []*Var{{Name: "VAR_PREFIX"}},
	}
	root := t.TempDir()
	writeFile(t, root, "shac.textproto", prototext.Format(config))

	main := "ctx-os-exec-passthrough_env.star"
	copyFile(t, root, filepath.Join("testdata", main))
	copyFile(t, root, filepath.Join("testdata", "ctx-os-exec-passthrough_env.go"))

	r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
	o := Options{
		Report:     &r,
		Dir:        root,
		EntryPoint: main,
		Vars:       map[string]string{"VAR_PREFIX": varPrefix},
	}

	const filesystemSandboxed = runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")

	wantLines := []string{
		"[//ctx-os-exec-passthrough_env.star:25] non-file env var: this is not a file",
		"read-only dir env var: " + env[readOnlyDirVarname],
		"writeable dir env var: " + env[writeableDirVarname],
		"able to write to writeable dir",
	}
	if filesystemSandboxed {
		wantLines = append(wantLines, fmt.Sprintf(
			"error writing to read-only dir: open %s: read-only file system",
			filepath.Join(env[readOnlyDirVarname], "foo.txt")))
	} else {
		wantLines = append(wantLines, "able to write to read-only dir")
	}

	if err := Run(context.Background(), &o); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(strings.Join(wantLines, "\n")+"\n", r.b.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestRun_Exec_InvalidPATHElements(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "file.txt", "")

	invalidPathElements := []string{
		filepath.Join(t.TempDir(), "does-not-exist"),
		"relative/path",
		"./dot/relative/path",
		filepath.Join(root, "file.txt"),
	}

	t.Setenv("PATH", strings.Join(
		append(invalidPathElements, os.Getenv("PATH")),
		string(os.PathListSeparator)))

	// No need for a complicated test case, just make sure that the subprocess
	// launching succeeds. If shac doesn't handle invalid PATH elements
	// correctly, launching any subprocess should fail, at least on platforms
	// where filesystem sandboxing is supported.
	cmd := "true"
	if runtime.GOOS == "windows" {
		cmd = "rundll32.exe"
	}
	writeFile(t, root, "shac.star", ""+
		"def cb(ctx):\n"+
		"    ctx.os.exec([\""+cmd+"\"]).wait()\n"+
		"    print(\"success!\")\n"+
		"shac.register_check(cb)\n")

	want := "[//shac.star:3] success!\n"
	testStarlarkPrint(t, root, "shac.star", false, false, want)
}

func TestRun_SCM_Raw(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a.txt", "First file")
	copySCM(t, root)
	t.Run("affected", func(t *testing.T) {
		t.Parallel()
		want := "[//ctx-scm-affected_files.star:19] \n" +
			"a.txt: \n" +
			scmStarlarkFiles("") +
			"\n"
		testStarlarkPrint(t, root, "ctx-scm-affected_files.star", false, false, want)
	})
	t.Run("affected-new_lines", func(t *testing.T) {
		t.Parallel()
		want := "[//ctx-scm-affected_files-new_lines.star:33] a.txt\n" +
			"1: First file\n"
		testStarlarkPrint(t, root, "ctx-scm-affected_files-new_lines.star", false, false, want)
	})
	t.Run("all", func(t *testing.T) {
		t.Parallel()
		want := "[//ctx-scm-all_files.star:19] \n" +
			"a.txt: \n" +
			scmStarlarkFiles("") +
			"\n"
		testStarlarkPrint(t, root, "ctx-scm-all_files.star", false, false, want)
	})
}

func scmStarlarkFiles(action string) string {
	return fmt.Sprintf("" +
		"ctx-scm-affected_files-include_deleted.star: " + action + "\n" +
		"ctx-scm-affected_files-new_lines.star: " + action + "\n" +
		"ctx-scm-affected_files.star: " + action + "\n" +
		"ctx-scm-all_files-include_deleted.star: " + action + "\n" +
		"ctx-scm-all_files.star: " + action + "\n",
	)
}

func TestRun_SCM_Git_NoUpstream_Pristine(t *testing.T) {
	// No upstream branch set, pristine checkout.
	t.Parallel()
	root := makeGit(t)
	copySCM(t, root)
	runGit(t, root, "add", "ctx-scm-*.star")
	runGit(t, root, "commit", "-m", "Third commit")

	data := []struct {
		name string
		all  bool
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			false,
			"[//ctx-scm-affected_files.star:19] \n" +
				scmStarlarkFiles("A") +
				"\n",
		},
		{
			"ctx-scm-affected_files.star",
			true,
			"[//ctx-scm-affected_files.star:19] \n" +
				"a.txt: \n" +
				scmStarlarkFiles("A") +
				"z.txt: \n" +
				"\n",
		},
		{
			"ctx-scm-all_files.star",
			false,
			"[//ctx-scm-all_files.star:19] \n" +
				"a.txt: \n" +
				scmStarlarkFiles("A") +
				"z.txt: \n" +
				"\n",
		},
	}
	for i := range data {
		i := i
		name := data[i].name
		if data[i].all {
			name += "/all"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, data[i].all, false, data[i].want)
		})
	}
}

func TestRun_SCM_Git_NoUpstream_Staged(t *testing.T) {
	// No upstream branch set, staged changes.
	t.Parallel()
	root := makeGit(t)
	copySCM(t, root)
	runGit(t, root, "add", "ctx-scm-*.star")

	data := []struct {
		name string
		all  bool
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			false,
			"[//ctx-scm-affected_files.star:19] \n" +
				scmStarlarkFiles("A") +
				"\n",
		},
		{
			"ctx-scm-affected_files-new_lines.star",
			false,
			"[//ctx-scm-affected_files-new_lines.star:33] ctx-scm-affected_files-include_deleted.star\n" +
				"1: # Copyright 2023 The Shac Authors\n",
		},
		{
			"ctx-scm-affected_files-new_lines.star",
			true,
			"[//ctx-scm-affected_files-new_lines.star:33] a.txt\n" +
				"1: First file\n",
		},
		{
			"ctx-scm-all_files.star",
			false,
			"[//ctx-scm-all_files.star:19] \n" +
				"a.txt: \n" +
				scmStarlarkFiles("A") +
				"z.txt: \n" +
				"\n",
		},
	}
	for i := range data {
		i := i
		name := data[i].name
		if data[i].all {
			name += "/all"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, data[i].all, false, data[i].want)
		})
	}
}

func TestRun_SCM_Git_Upstream_Staged(t *testing.T) {
	// Upstream set, staged changes.
	t.Parallel()
	root := makeGit(t)
	runGit(t, root, "checkout", "-b", "up", "HEAD~1")
	runGit(t, root, "checkout", "master")
	runGit(t, root, "branch", "--set-upstream-to", "up")
	copySCM(t, root)
	runGit(t, root, "add", "ctx-scm-*.star")

	data := []struct {
		name string
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			"[//ctx-scm-affected_files.star:19] \n" +
				"a.txt: R\n" +
				scmStarlarkFiles("A") +
				"z.txt: A\n" +
				"\n",
		},
		{
			"ctx-scm-all_files.star",
			"[//ctx-scm-all_files.star:19] \n" +
				"a.txt: R\n" +
				scmStarlarkFiles("A") +
				"z.txt: A\n" +
				"\n",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, false, false, data[i].want)
		})
	}
}

func TestRun_SCM_Git_Untracked(t *testing.T) {
	t.Parallel()

	data := []struct {
		name string
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			"[//shac.star:19] \n" +
				".gitignore: A\n" +
				"shac.star: A\n" +
				"staged.txt: A\n" +
				"untracked.txt: A\n" +
				"\n",
		},
		{
			"ctx-scm-all_files.star",
			"[//shac.star:19] \n" +
				".gitignore: \n" +
				"a.txt: \n" +
				"shac.star: \n" +
				"staged.txt: A\n" +
				"untracked.txt: \n" +
				"z.txt: \n" +
				"\n",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()

			// Use a separate root for each test case because we write a
			// different `shac.star` file to the root for each test case, so
			// they wouldn't be safe to run in parallel otherwise.
			root := makeGit(t)

			// git-ignored files should not be included.
			writeFile(t, root, "ignored.txt", "This file should be ignored")
			writeFile(t, root, ".gitignore", "ignored.txt\n")

			// Even if one file is staged, untracked files should still be considered.
			writeFile(t, root, "staged.txt", "This is a staged file")
			runGit(t, root, "add", "staged.txt")

			writeFile(t, root, "untracked.txt", "This is an untracked file")

			// Instead of running the testdata file directly, copy it to an
			// untracked `shac.star` file to ensure that untracked shac.star
			// files can be discovered and run.
			d, err := os.ReadFile(filepath.Join("testdata", "scm", data[i].name))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "shac.star"), d, 0o600); err != nil {
				t.Fatal(err)
			}

			r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
			// Don't specify `main` so it defaults to shac.star.
			// Specify `recurse` so we use the scm to discover shac.star files.
			o := Options{Report: &r, Dir: root, Recurse: true}
			if err = Run(context.Background(), &o); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(data[i].want, r.b.String()); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
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
	runGit(t, root, "add", "ctx-scm-*.star")

	data := []struct {
		name string
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			"[//ctx-scm-affected_files.star:19] \n" +
				".gitmodules: A\n" +
				scmStarlarkFiles("A") +
				"\n",
		},
		{
			"ctx-scm-all_files.star",
			"[//ctx-scm-all_files.star:19] \n" +
				".gitmodules: A\n" +
				"a.txt: \n" +
				scmStarlarkFiles("A") +
				"z.txt: \n" +
				"\n",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, false, false, data[i].want)
		})
	}
}

func TestRun_SCM_DeletedFile(t *testing.T) {
	t.Parallel()

	root := makeGit(t)
	copySCM(t, root)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Initial commit")

	writeFile(t, root, "file-to-delete.txt", "This file will be deleted")
	runGit(t, root, "add", "file-to-delete.txt")
	runGit(t, root, "commit", "-m", "Add file-to-delete.txt")

	if err := os.Remove(filepath.Join(root, "file-to-delete.txt")); err != nil {
		t.Fatal(err)
	}

	data := []struct {
		name string
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			"[//ctx-scm-affected_files.star:19] \n\n",
		},
		{
			"ctx-scm-affected_files-include_deleted.star",
			"[//ctx-scm-affected_files-include_deleted.star:35] \n" +
				"With deleted:\n" +
				"file-to-delete.txt (D): ()\n" +
				"\n" +
				"Without deleted:\n" +
				"\n"},
		{
			"ctx-scm-all_files.star",
			"[//ctx-scm-all_files.star:19] \n" +
				"a.txt: \n" +
				scmStarlarkFiles("") +
				"z.txt: \n" +
				"\n",
		},
		{
			"ctx-scm-all_files-include_deleted.star",
			"[//ctx-scm-all_files-include_deleted.star:26] \n" +
				"With deleted:\n" +
				"a.txt: \n" +
				scmStarlarkFiles("") +
				"file-to-delete.txt: D\n" +
				"z.txt: \n" +
				"\n" +
				"Without deleted:\n" +
				"a.txt: \n" +
				scmStarlarkFiles("") +
				"z.txt: \n" +
				"\n"},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, false, false, data[i].want)
		})
	}
}

func TestRun_SCM_Git_Binary_File(t *testing.T) {
	t.Parallel()
	root := makeGit(t)

	copySCM(t, root)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Initial commit")

	// Git considers a file to be binary if it contains a null byte.
	writeFileBytes(t, root, "a.bin", []byte{0, 1, 2, 3}, 0o600)
	runGit(t, root, "add", "a.bin")

	data := []struct {
		name string
		all  bool
		want string
	}{
		{
			"ctx-scm-affected_files.star",
			false,
			"[//ctx-scm-affected_files.star:19] \n" +
				"a.bin: A\n" +
				"\n",
		},
		{
			"ctx-scm-affected_files-new_lines.star",
			false,
			// Only a binary file is touched, no lines should be considered
			// affected.
			"[//ctx-scm-affected_files-new_lines.star:35] no new lines\n",
		},
		{
			"ctx-scm-affected_files-new_lines.star",
			true,
			"[//ctx-scm-affected_files-new_lines.star:35] no new lines\n",
		},
	}

	for i := range data {
		i := i
		name := data[i].name
		if data[i].all {
			name += "/all"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testStarlarkPrint(t, root, data[i].name, data[i].all, false, data[i].want)
		})
	}
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
	o := Options{Report: &reportNoPrint{t: t}, Dir: root, EntryPoint: "ctx-scm-affected_files.star"}
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
	root := resolvedTempDir(t)
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
		"      ctx.emit.finding(level=\"notice\", message=name, filepath=p)\n"+
		"    else:\n"+
		"      print(name + \": \" + p)\n"+
		"shac.register_check(cb)\n")
	writeFile(t, root, "a/shac.star", ""+
		"def cb(ctx):\n"+
		"  name = \"a\"\n"+
		"  for p, m in ctx.scm.affected_files().items():\n"+
		"    if p.endswith(\".txt\"):\n"+
		"      print(name + \": \" + p + \"=\" + m.new_lines()[0][1])\n"+
		"      ctx.emit.finding(level=\"notice\", message=name, filepath=p)\n"+
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
		"      ctx.emit.finding(level=\"notice\", message=name, filepath=p)\n"+
		"    else:\n"+
		"      print(name + \": \" + p)\n"+
		"shac.register_check(cb)\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Second commit")
	r := reportEmitPrint{reportPrint: reportPrint{reportNoPrint: reportNoPrint{t: t}}}
	o := Options{Report: &r, Dir: root, Recurse: true}
	if err := Run(context.Background(), &o); err != nil {
		t.Fatal(err)
	}
	// a/a.txt is skipped because it was in the first commit.
	// shac.star see all files.
	// a/shac.star only see files in a/.
	// b/shac.star only see files in b/.
	want := "\n" +
		"[//a/shac.star:8] a: shac.star\n" +
		"[//b/shac.star:5] b: b.txt=content b\n" +
		"[//b/shac.star:8] b: shac.star\n" +
		"[//shac.star:5] root: b/b.txt=content b\n" +
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
	findings := []finding{
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
	sort.Slice(r.findings, func(i, j int) bool { return r.findings[i].File < r.findings[j].File })
	if diff := cmp.Diff(findings, r.findings); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestRun_SCM_Git_Recursive_Shared(t *testing.T) {
	t.Parallel()
	// Tree content:
	//   a/
	//     shac.star
	//     a.txt  (not affected)
	//     b.txt
	//   common/
	//     c.txt
	//     shared.star
	//     internal/
	//       internal.star
	//   d/
	//     shared2.star
	root := resolvedTempDir(t)
	initGit(t, root)
	for _, p := range [...]string{"a", "common", filepath.Join("common", "internal"), "d"} {
		if err := os.Mkdir(filepath.Join(root, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// a/a.txt is in the initial commit, thus is not affected in commit HEAD.
	writeFile(t, root, "a/a.txt", "content a")
	runGit(t, root, "add", "a/a.txt")
	runGit(t, root, "commit", "-m", "Initial commit")

	// The affected files:
	writeFile(t, root, "a/b.txt", "content b")
	writeFile(t, root, "a/shac.star", ""+
		// Loads a file relative to the root.
		"load(\"//common/shared.star\", \"cb\")\n"+
		"shac.register_check(cb)\n")
	writeFile(t, root, "common/shared.star", ""+
		// Loads a file relative to the current directory.
		"load(\"internal/internal.star\", \"cbinner\")\n"+
		"cb = cbinner")
	writeFile(t, root, "common/c.txt", "content c")
	writeFile(t, root, "common/internal/internal.star", ""+
		// Loads a file relative to the current directory, going higher up.
		"load(\"../../d/shared2.star\", \"cb\")\n"+
		"cbinner = cb")
	writeFile(t, root, "d/shared2.star", ""+
		// This function sees the files affected from the perspective of the
		// importer, which is a/shac.star.
		"def cb(ctx):\n"+
		"  for p, m in ctx.scm.affected_files().items():\n"+
		"    if p.endswith(\".txt\"):\n"+
		"      print(p + \"=\" + m.new_lines()[0][1])\n"+
		"      ctx.emit.finding(level=\"notice\", message=\"internal\", filepath=p)\n"+
		"    else:\n"+
		"      print(p)\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "Second commit")
	r := reportEmitPrint{reportPrint: reportPrint{reportNoPrint: reportNoPrint{t: t}}}
	o := Options{Report: &r, Dir: root, Recurse: true}
	if err := Run(context.Background(), &o); err != nil {
		t.Fatal(err)
	}
	// a/a.txt is skipped because it was in the first commit.
	// a/shac.star only see files in a/.
	want := "\n" +
		"[//d/shared2.star:4] b.txt=content b\n" +
		"[//d/shared2.star:7] shac.star"
	// With parallel execution, the output will not be deterministic. Sort it manually.
	a := strings.Split(r.b.String(), "\n")
	sort.Strings(a)
	got := strings.Join(a, "\n")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	findings := []finding{
		{
			Check:   "cb",
			Level:   "notice",
			Message: "internal",
			Root:    filepath.Join(root, "a"),
			File:    "b.txt",
		},
	}
	// With parallel execution, the output will not be deterministic. Sort it manually.
	sort.Slice(r.findings, func(i, j int) bool { return r.findings[i].File < r.findings[j].File })
	if diff := cmp.Diff(findings, r.findings); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

// TestTestDataFailOrThrow runs all the files under testdata/fail_or_throw/.
//
// These test cases call fail() or throw an exception.
func TestTestDataFailOrThrow(t *testing.T) {
	t.Parallel()
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
				"  //backtrace.star:19:8: in fn1\n" +
				"  //backtrace.star:16:9: in fn2\n",
		},
		{
			"ctx-emit-artifact-dir.star",
			"ctx.emit.artifact: for parameter \"filepath\": \".\" is a directory",
			"  //ctx-emit-artifact-dir.star:16:22: in cb\n",
		},
		{
			"ctx-emit-artifact-inexistant.star",
			"ctx.emit.artifact: for parameter \"filepath\": \"inexistant\" not found",
			"  //ctx-emit-artifact-inexistant.star:16:22: in cb\n",
		},
		{
			"ctx-emit-artifact-kwarg.star",
			"ctx.emit.artifact: unexpected keyword argument \"foo\"",
			"  //ctx-emit-artifact-kwarg.star:16:22: in cb\n",
		},
		{
			"ctx-emit-artifact-type.star",
			"ctx.emit.artifact: for parameter \"content\": got int, want str or bytes",
			"  //ctx-emit-artifact-type.star:16:22: in cb\n",
		},
		{
			"ctx-emit-artifact-windows.star",
			"ctx.emit.artifact: for parameter \"filepath\": \"foo\\\\bar\" use POSIX style path",
			"  //ctx-emit-artifact-windows.star:16:22: in cb\n",
		},
		{
			"ctx-emit-finding-col-line.star",
			"ctx.emit.finding: for parameter \"col\": \"line\" must be specified",
			"  //ctx-emit-finding-col-line.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-col.star",
			"ctx.emit.finding: for parameter \"col\": got -10, line are 1 based",
			"  //ctx-emit-finding-col.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-end_col-col-equal.star",
			"ctx.emit.finding: for parameter \"end_col\": \"end_col\" (2) must be greater than \"col\" (2)",
			"  //ctx-emit-finding-end_col-col-equal.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-end_col-col.star",
			"ctx.emit.finding: for parameter \"end_col\": \"col\" must be specified",
			"  //ctx-emit-finding-end_col-col.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-end_col.star",
			"ctx.emit.finding: for parameter \"end_col\": got -10, line are 1 based",
			"  //ctx-emit-finding-end_col.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-end_line-line-reverse.star",
			"ctx.emit.finding: for parameter \"end_line\": \"end_line\" (1) must be greater than or equal to \"line\" (2)",
			"  //ctx-emit-finding-end_line-line-reverse.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-end_line-line.star",
			"ctx.emit.finding: for parameter \"end_line\": \"line\" must be specified",
			"  //ctx-emit-finding-end_line-line.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-end_line.star",
			"ctx.emit.finding: for parameter \"end_line\": got -10, line are 1 based",
			"  //ctx-emit-finding-end_line.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-kwarg.star",
			"ctx.emit.finding: unexpected keyword argument \"foo\"",
			"  //ctx-emit-finding-kwarg.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-level.star",
			"ctx.emit.finding: for parameter \"level\": got \"invalid\", want one of \"notice\", \"warning\" or \"error\"",
			"  //ctx-emit-finding-level.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-line-no_file.star",
			"ctx.emit.finding: for parameter \"line\": \"filepath\" must be specified",
			"  //ctx-emit-finding-line-no_file.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-line.star",
			"ctx.emit.finding: for parameter \"line\": got -1, line are 1 based",
			"  //ctx-emit-finding-line.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-message.star",
			"ctx.emit.finding: for parameter \"message\": must not be empty",
			"  //ctx-emit-finding-message.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-replacements-limit.star",
			"ctx.emit.finding: for parameter \"replacements\": excessive number (101) of replacements",
			"  //ctx-emit-finding-replacements-limit.star:17:21: in cb\n",
		},
		{
			"ctx-emit-finding-replacements-list.star",
			"ctx.emit.finding: for parameter \"replacements\": got list, want sequence of str",
			"  //ctx-emit-finding-replacements-list.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-replacements-no_file.star",
			"ctx.emit.finding: for parameter \"replacements\": \"filepath\" must be specified",
			"  //ctx-emit-finding-replacements-no_file.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-replacements-str.star",
			"ctx.emit.finding: for parameter \"replacements\": got string, want starlark.Sequence",
			"  //ctx-emit-finding-replacements-str.star:16:21: in cb\n",
		},
		{
			"ctx-emit-finding-replacements-tuple.star",
			"ctx.emit.finding: for parameter \"replacements\": got tuple, want sequence of str",
			"  //ctx-emit-finding-replacements-tuple.star:16:21: in cb\n",
		},
		{
			"ctx-immutable.star",
			"can't assign to .key field of struct",
			"  //ctx-immutable.star:17:8: in cb\n",
		},
		{
			"ctx-io-read_file-dir.star",
			"ctx.io.read_file: for parameter \"filepath\": \".\" is a directory",
			"  //ctx-io-read_file-dir.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-escape.star",
			"ctx.io.read_file: for parameter \"filepath\": \"../checks.go\" cannot escape root",
			"  //ctx-io-read_file-escape.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-inexistant.star",
			"ctx.io.read_file: for parameter \"filepath\": \"inexistant\" not found",
			"  //ctx-io-read_file-inexistant.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-missing_arg.star",
			"ctx.io.read_file: missing argument for filepath",
			"  //ctx-io-read_file-missing_arg.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-size_big.star",
			"ctx.io.read_file: for parameter \"size\": 36893488147419103232 is an invalid size",
			"  //ctx-io-read_file-size_big.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-size_type.star",
			"ctx.io.read_file: for parameter \"size\": got string, want int",
			"  //ctx-io-read_file-size_type.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-unclean.star",
			"ctx.io.read_file: for parameter \"filepath\": \"path/../file.txt\" pass cleaned path",
			"  //ctx-io-read_file-unclean.star:16:21: in cb\n",
		},
		{
			"ctx-io-read_file-windows.star",
			"ctx.io.read_file: for parameter \"filepath\": \"test\\\\data.txt\" use POSIX style path",
			"  //ctx-io-read_file-windows.star:16:21: in cb\n",
		},
		{
			"ctx-os-exec-10Mib-exceed.star",
			"wait: process returned too much stderr",
			"  //ctx-os-exec-10Mib-exceed.star:16:93: in cb\n",
		},
		{
			"ctx-os-exec-bad_arg.star",
			"ctx.os.exec: unexpected keyword argument \"unknown\"",
			"  //ctx-os-exec-bad_arg.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-bad_env_key.star",
			"ctx.os.exec: \"env\" key is not a string: 1",
			"  //ctx-os-exec-bad_env_key.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-bad_env_value.star",
			"ctx.os.exec: \"env\" value is not a string: 1",
			"  //ctx-os-exec-bad_env_value.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-bad_stdin_type.star",
			"ctx.os.exec: for parameter \"stdin\": got dict, want str or bytes",
			"  //ctx-os-exec-bad_stdin_type.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-bad_type_in_args.star",
			"ctx.os.exec: for parameter \"cmd\": got list, want sequence of str",
			"  //ctx-os-exec-bad_type_in_args.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-command_not_in_path.star",
			func() string {
				if runtime.GOOS == "windows" {
					return "ctx.os.exec: exec: \"this-command-does-not-exist\": executable file not found in %PATH%"
				}
				return "ctx.os.exec: exec: \"this-command-does-not-exist\": executable file not found in $PATH"
			}(),
			"  //ctx-os-exec-command_not_in_path.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-double_wait.star",
			"wait: wait was already called",
			"  //ctx-os-exec-double_wait.star:21:14: in cb\n",
		},
		{
			"ctx-os-exec-false.star",
			func() string {
				if runtime.GOOS == "windows" {
					return "wait: command failed with exit code 1: [cmd.exe /c exit 1]"
				}
				return "wait: command failed with exit code 1: [false]"
			}(),
			"  //ctx-os-exec-false.star:19:26: in cb\n",
		},
		{
			"ctx-os-exec-invalid_cwd.star",
			"ctx.os.exec: cannot escape root",
			"  //ctx-os-exec-invalid_cwd.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-mutate_result.star",
			"can't assign to .retcode field of struct",
			"  //ctx-os-exec-mutate_result.star:20:8: in cb\n",
		},
		{
			"ctx-os-exec-no_cmd.star",
			"ctx.os.exec: cmdline must not be an empty list",
			"  //ctx-os-exec-no_cmd.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-no_wait.star",
			func() string {
				cmd := "echo hello world"
				if runtime.GOOS == "windows" {
					cmd = "cmd.exe /c " + cmd
				}
				return "wait() was not called on <subprocess \"" + cmd + "\">"
			}(),
			"",
		},
		{
			"ctx-os-exec-ok_retcodes.star",
			func() string {
				if runtime.GOOS == "windows" {
					return "wait: command failed with exit code 0: [cmd.exe /c exit 0]"
				}
				return "wait: command failed with exit code 0: [true]"
			}(),
			"  //ctx-os-exec-ok_retcodes.star:28:45: in cb\n",
		},
		{
			"ctx-os-exec-ok_retcodes_invalid_element.star",
			"ctx.os.exec: for parameter \"ok_retcodes\": got [0, \"blah\"], wanted sequence of ints",
			"  //ctx-os-exec-ok_retcodes_invalid_element.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-ok_retcodes_invalid_type.star",
			"ctx.os.exec: for parameter \"ok_retcodes\": got \"blah\", wanted sequence of ints",
			"  //ctx-os-exec-ok_retcodes_invalid_type.star:16:16: in cb\n",
		},
		{
			"ctx-os-exec-result_unhashable.star",
			"unhashable type: subprocess",
			"  //ctx-os-exec-result_unhashable.star:20:16: in cb\n",
		},
		{
			"ctx-re-allmatches-no_arg.star",
			"ctx.re.allmatches: missing argument for pattern",
			"  //ctx-re-allmatches-no_arg.star:16:22: in cb\n",
		},
		{
			"ctx-re-match-bad_re.star",
			"ctx.re.match: error parsing regexp: missing closing ): `(`",
			"  //ctx-re-match-bad_re.star:16:17: in cb\n",
		},
		{
			"ctx-re-match-no_arg.star",
			"ctx.re.match: missing argument for pattern",
			"  //ctx-re-match-no_arg.star:16:17: in cb\n",
		},
		{
			"ctx-scm-affected_files-arg.star",
			"ctx.scm.affected_files: for parameter include_deleted: got string, want bool",
			"  //ctx-scm-affected_files-arg.star:16:27: in cb\n",
		},
		{
			"ctx-scm-affected_files-kwarg.star",
			"ctx.scm.affected_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-affected_files-kwarg.star:16:27: in cb\n",
		},
		{
			"ctx-scm-all_files-arg.star",
			"ctx.scm.all_files: for parameter include_deleted: got string, want bool",
			"  //ctx-scm-all_files-arg.star:16:22: in cb\n",
		},
		{
			"ctx-scm-all_files-kwarg.star",
			"ctx.scm.all_files: unexpected keyword argument \"unexpected\"",
			"  //ctx-scm-all_files-kwarg.star:16:22: in cb\n",
		},
		{
			"ctx-vars-empty.star",
			"ctx.vars.get: for parameter \"name\": must not be empty",
			"  //ctx-vars-empty.star:16:17: in cb\n",
		},
		{
			"ctx-vars-invalid.star",
			"ctx.vars.get: unknown variable \"invalid_var\"",
			"  //ctx-vars-invalid.star:16:17: in cb\n",
		},
		{
			"empty.star",
			"did you forget to call shac.register_check?",
			"",
		},
		{
			"exec-file.star",
			"fail: undefined: exec",
			"  //exec-file.star:15:1: in <toplevel>\n",
		},
		{
			"exec-statement.star",
			"fail: undefined: exec",
			"  //exec-statement.star:15:1: in <toplevel>\n",
		},
		{
			"fail-check.star",
			"fail: an  unexpected  failure  None\nfail: unexpected keyword argument \"unknown\"",
			"  //fail-check.star:16:9: in cb\n",
		},
		{
			"fail.star",
			"fail: an expected failure",
			"  //fail.star:15:5: in <toplevel>\n",
		},
		{
			"load-from_check.star",
			"fail: load statement within a function",
			// It's a bit sad that the function name is not printed out. This is
			// because this error happens at syntax parsing phase, not at execution
			// phase.
			"  //load-from_check.star:16:5: in <toplevel>\n",
		},
		{
			"load-inexistant.star",
			"cannot load ./inexistant.star: inexistant.star not found",
			"  //load-inexistant.star:15:1: in <toplevel>\n",
		},
		{
			"load-no_symbol.star",
			"//load-no_symbol.star:15:5: load statement must import at least 1 symbol",
			"",
		},
		{
			"load-pkg_inexistant.star",
			"cannot load @inexistant: package not found",
			"  //load-pkg_inexistant.star:15:1: in <toplevel>\n",
		},
		{
			"load-recurse.star",
			"cannot load ./load-recurse.star: //load-recurse.star was loaded in a cycle dependency graph",
			"  //load-recurse.star:15:1: in <toplevel>\n",
		},
		{
			"shac-check-star_args.star",
			"shac.check: \"impl\" must not accept *args",
			"  //shac-check-star_args.star:18:31: in <toplevel>\n",
		},
		{
			"shac-check-star_kwargs.star",
			"shac.check: \"impl\" must not accept **kwargs",
			"  //shac-check-star_kwargs.star:18:31: in <toplevel>\n",
		},
		{
			"shac-check-with_args-ctx.star",
			"with_args: \"ctx\" argument cannot be overridden",
			"  //shac-check-with_args-ctx.star:18:45: in <toplevel>\n",
		},
		{
			"shac-check-with_args-nonexistent_kwarg.star",
			"with_args: invalid argument \"nonexistent\", must be one of: (foo)",
			"  //shac-check-with_args-nonexistent_kwarg.star:18:45: in <toplevel>\n",
		},
		{
			"shac-check-with_args-positional_args.star",
			"with_args: only keyword arguments are allowed",
			"  //shac-check-with_args-positional_args.star:18:45: in <toplevel>\n",
		},
		{
			"shac-immutable.star",
			"can't assign to .key field of struct",
			"  //shac-immutable.star:16:5: in <toplevel>\n",
		},
		{
			"shac-register_check-bad_ctx_name.star",
			"shac.register_check: \"impl\"'s first parameter must be named \"ctx\"",
			"  //shac-register_check-bad_ctx_name.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-bad_type.star",
			"shac.register_check: \"check\" must be a function or shac.check object, got string",
			"  //shac-register_check-bad_type.star:15:20: in <toplevel>\n",
		},
		{
			"shac-register_check-builtin.star",
			"shac.register_check: \"impl\" must not be a built-in function",
			"  //shac-register_check-builtin.star:15:20: in <toplevel>\n",
		},
		{
			"shac-register_check-callback_without_arguments.star",
			"shac.register_check: \"impl\" must be a function accepting one \"ctx\" argument",
			"  //shac-register_check-callback_without_arguments.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-default_ctx.star",
			"shac.register_check: \"impl\" must not have a default value for the \"ctx\" parameter",
			"  //shac-register_check-default_ctx.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-kwarg.star",
			"shac.register_check: unexpected keyword argument \"invalid\"",
			"  //shac-register_check-kwarg.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-lambda.star",
			"shac.register_check: \"name\" must be set when \"impl\" is a lambda",
			"  //shac-register_check-lambda.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-multiple_required_args.star",
			"shac.register_check: \"impl\" cannot have required arguments besides \"ctx\"",
			"  //shac-register_check-multiple_required_args.star:18:20: in <toplevel>\n",
		},
		{
			"shac-register_check-no_arg.star",
			"shac.register_check: missing argument for check",
			"  //shac-register_check-no_arg.star:15:20: in <toplevel>\n",
		},
		{
			"shac-register_check-recursive.star",
			"shac.register_check: can't register checks after done loading",
			"  //shac-register_check-recursive.star:19:24: in cb1\n",
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
			"//syntax_error.star:15:1: got '//', want primary expression",
			"",
		},
		{
			"undefined_symbol.star",
			"fail: undefined: undefined_symbol",
			"  //undefined_symbol.star:15:1: in <toplevel>\n",
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
			o := Options{Report: &reportNoPrint{t: t}, Dir: root, EntryPoint: data[i].name}
			err := Run(context.Background(), &o)
			if err == nil {
				t.Fatal("expecting an error")
			}
			if diff := cmp.Diff(data[i].err, err.Error()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			expectTrace := data[i].trace != ""
			var err2 BacktraceableError
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

func TestRun_NetworkSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" &&
		!(runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")) {
		t.Skipf("Network sandboxing not supported on platform %s-%s", runtime.GOOS, runtime.GOARCH)
	}
	t.Parallel()

	responseBody := "Hello from the server!"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, responseBody)
	})

	tmpl, err := template.New("shac.star").Parse(
		readFile(t, filepath.Join("testdata", "ctx-os-exec-network_sandbox.template.star")))
	if err != nil {
		t.Fatal(err)
	}

	data := []struct {
		name         string
		allowNetwork bool
		want         string
	}{
		{
			"network blocked",
			false,
			"[//shac.star:22] Exit code: 1\n" +
				"[//shac.star:23] Network unavailable\n\n",
		},
		{
			"network allowed",
			true,
			"[//shac.star:23] " + responseBody + "\n",
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(handler)
			defer server.Close()

			var w strings.Builder
			tmpl.Execute(&w, struct {
				ServerURL    string
				AllowNetwork bool
			}{
				ServerURL:    server.URL,
				AllowNetwork: data[i].allowNetwork,
			})

			root := t.TempDir()
			writeFile(t, root, "shac.star", w.String())
			writeFile(t, root, "http_get.sh", readFile(t, filepath.Join("testdata", "http_get.sh")))
			if err := os.Chmod(filepath.Join(root, "http_get.sh"), 0o700); err != nil {
				t.Fatal(err)
			}
			testStarlarkPrint(t, root, "shac.star", false, false, data[i].want)
		})
	}
}

// TestTestDataEmit runs all the files under testdata/emit/.
func TestTestDataEmit(t *testing.T) {
	t.Parallel()
	root, got := enumDir(t, "emit")
	data := []struct {
		name      string
		findings  []finding
		artifacts []artifact
		err       string
	}{
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
		{
			"ctx-emit-finding-error.star",
			[]finding{
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
			"ctx-emit-finding-none.star",
			[]finding{
				{
					Check:   "cb",
					Level:   Error,
					Message: "bad code",
					Root:    root,
					File:    "file.txt",
				},
			},
			nil,
			"a check failed",
		},
		{
			"ctx-emit-finding.star",
			[]finding{
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
					Root:    root,
					File:    "file.txt",
					Span:    Span{Start: Cursor{Line: 100, Col: 2}},
				}, {
					Check:   "cb",
					Level:   "notice",
					Message: "nice",
					Root:    root,
					File:    "file.txt",
					Span:    Span{Start: Cursor{Line: 100, Col: 2}, End: Cursor{Line: 100, Col: 3}},
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
					Root:         root,
					File:         "file.txt",
					Span:         Span{Start: Cursor{Line: 1}, End: Cursor{Line: 10}},
					Replacements: []string{"a", "dict"},
				},
				{
					Check:        "cb",
					Level:        "warning",
					Message:      "no span, full file",
					Root:         root,
					File:         "file.txt",
					Replacements: []string{"this text is a replacement\nfor the entire file\n"},
				},
			},
			nil,
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
			r := reportEmitNoPrint{reportNoPrint: reportNoPrint{t: t}}
			o := Options{Report: &r, Dir: root, EntryPoint: data[i].name, config: "../config/valid.textproto"}
			err := Run(context.Background(), &o)
			if data[i].err != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				got := err.Error()
				if data[i].err != got {
					t.Fatal(got)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(data[i].findings, r.findings); diff != "" {
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
	v := fmt.Sprintf("(%d, %d, %d)", Version[0], Version[1], Version[2])
	data := []struct {
		name        string
		want        string
		ignoreOrder bool
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
			name: "ctx-io-tempdir.star",
			want: func() string {
				if runtime.GOOS == "windows" {
					return "[//ctx-io-tempdir.star:16] \\0\\0\n" +
						"[//ctx-io-tempdir.star:17] \\0\\1\n" +
						"[//ctx-io-tempdir.star:18] \\0\\2\n"
				}
				return "[//ctx-io-tempdir.star:16] /0/0\n" +
					"[//ctx-io-tempdir.star:17] /0/1\n" +
					"[//ctx-io-tempdir.star:18] /0/2\n"
			}(),
		},
		{
			name: "ctx-io-tempfile.star",
			want: "[//ctx-io-tempfile.star:18] first\nfile\ncontents\n\n" +
				"[//ctx-io-tempfile.star:19] contents\nof\nsecond\nfile\n\n",
		},
		{
			name: "ctx-os-exec-10Mib.star",
			want: "[//ctx-os-exec-10Mib.star:17] 0\n",
		},
		{
			name: "ctx-os-exec-abspath.star",
			want: func() string {
				// TODO(maruel): Decide if we want to do CRLF translation automatically.
				if runtime.GOOS == "windows" {
					return "[//ctx-os-exec-abspath.star:17] Hello, world\r\n\n"
				}
				return "[//ctx-os-exec-abspath.star:17] Hello, world\n\n"
			}(),
		},
		{
			name: "ctx-os-exec-env.star",
			want: func() string {
				// TODO(maruel): Decide if we want to do CRLF translation automatically.
				if runtime.GOOS == "windows" {
					return "[//ctx-os-exec-env.star:24] FOO=foo-value\r\nBAR=bar-value\n"
				}
				return "[//ctx-os-exec-env.star:24] FOO=foo-value\nBAR=bar-value\n"
			}(),
		},
		{
			name: "ctx-os-exec-parallel.star",
			want: strings.Repeat("[//ctx-os-exec-parallel.star:32] Hello, world\n", 1000),
		},
		{
			name: "ctx-os-exec-relpath.star",
			want: func() string {
				// TODO(maruel): Decide if we want to do CRLF translation automatically.
				if runtime.GOOS == "windows" {
					return "[//ctx-os-exec-relpath.star:17] Hello from a nested file\r\n\n"
				}
				return "[//ctx-os-exec-relpath.star:17] Hello from a nested file\n\n"
			}(),
		},
		{
			name: "ctx-os-exec-stdin.star",
			want: "[//ctx-os-exec-stdin.star:30] stdout given NoneType for stdin:\n" +
				"\n" +
				"[//ctx-os-exec-stdin.star:30] stdout given string for stdin:\n" +
				"hello\nfrom\nstdin\nstring\n" +
				"[//ctx-os-exec-stdin.star:30] stdout given bytes for stdin:\n" +
				"hello\nfrom\nstdin\nbytes\n",
		},
		{
			name: "ctx-os-exec-success.star",
			want: "[//ctx-os-exec-success.star:21] retcode: 0\n" +
				"[//ctx-os-exec-success.star:22] stdout: hello from stdout\n" +
				"[//ctx-os-exec-success.star:23] stderr: hello from stderr\n",
		},
		{
			name: "ctx-platform.star",
			want: "[//ctx-platform.star:16] OS: " + runtime.GOOS + "\n" +
				"[//ctx-platform.star:17] Arch: " + runtime.GOARCH + "\n",
		},
		{
			name: "ctx-re-allmatches.star",
			want: "[//ctx-re-allmatches.star:17] ()\n" +
				"[//ctx-re-allmatches.star:20] (match(groups = (\"TODO(foo)\",), offset = 4), match(groups = (\"TODO(bar)\",), offset = 14))\n" +
				"[//ctx-re-allmatches.star:23] (match(groups = (\"anc\", \"n\", \"c\"), offset = 0),)\n",
		},
		{
			name: "ctx-re-match.star",
			want: "[//ctx-re-match.star:17] None\n" +
				"[//ctx-re-match.star:20] match(groups = (\"TODO(foo)\",), offset = 4)\n" +
				"[//ctx-re-match.star:23] match(groups = (\"anc\", \"n\", \"c\"), offset = 0)\n" +
				"[//ctx-re-match.star:26] match(groups = (\"a\", None), offset = 0)\n",
		},
		{
			name: "dir-ctx.star",
			want: "[//dir-ctx.star:16] [\"emit\", \"io\", \"os\", \"platform\", \"re\", \"scm\", \"vars\"]\n",
		},
		{
			name: "dir-shac.star",
			want: "[//dir-shac.star:15] [\"check\", \"commit_hash\", \"register_check\", \"version\"]\n",
		},
		{
			name: "load-diamond_dependency.star",
			want: "[//load-diamond_dependency.star:18] i am a constant\n" +
				"[//load-diamond_dependency.star:19] i am a constant #2\n",
		},
		{
			name: "optional-features.star",
			want: "[//optional-features.star:17] recursion complete\n" +
				"[//optional-features.star:23] set([\"foo\"])\n",
		},
		{
			name: "print-shac-version.star",
			want: "[//print-shac-version.star:15] " + v + "\n",
		},
		{
			name: "shac-check-with_args.star",
			want: "[//shac-check-with_args.star:33] print_hello_check: <check print_hello>\n" +
				"[//shac-check-with_args.star:34] print_goodbye_check: <check print_goodbye>\n" +
				"[//shac-check-with_args.star:35] print_hello_again_check: <check print_hello_again>\n" +
				"[//shac-check-with_args.star:16] hello again\n" +
				"[//shac-check-with_args.star:16] goodbye\n" +
				"[//shac-check-with_args.star:16] hello\n",
			// Ordering of output lines depends on the order in which checks are
			// run, which is nondeterministic and doesn't matter for the
			// purposes of this test.
			ignoreOrder: true,
		},
		{
			name: "shac-check.star",
			want: "[//shac-check.star:19] str(check): <check hello_world>\n" +
				"[//shac-check.star:20] type(check): shac.check\n" +
				"[//shac-check.star:21] bool(check): True\n" +
				"[//shac-check.star:26] hashed: set([<check hello_world>])\n",
		},
		{
			name: "shac-register_check-object.star",
			want: "[//shac-register_check-object.star:16] running from a check object\n",
		},
		{
			name: "shac-register_check-optional_param.star",
			want: "[//shac-register_check-optional_param.star:16] optional_param=\"optional-param-value\"\n",
		},
		{
			name: "shac-register_check.star",
			want: "[//shac-register_check.star:16] running\n",
		},
		{
			name: "subprocess.star",
			want: func() string {
				cmd := "echo hello world"
				if runtime.GOOS == "windows" {
					cmd = "cmd.exe /c " + cmd
				}
				return "[//subprocess.star:20] str(proc): <subprocess \"" + cmd + "\">\n" +
					"[//subprocess.star:21] type(proc): subprocess\n" +
					"[//subprocess.star:22] bool(proc): True\n" +
					"[//subprocess.star:23] dir(proc): [\"wait\"]\n"
			}(),
		},
		{
			name: "true.star",
			want: "[//true.star:15] True\n",
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
			testStarlarkPrint(t, p, data[i].name, false, data[i].ignoreOrder, data[i].want)
		})
	}
}

func TestRun_FilesystemSandbox(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
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

	writeFile(t, root, "sandbox_read.sh", ""+
		"#!/bin/sh\n"+
		"set -e\n"+
		"cat \""+fileOutsideRoot+"\"\n")
	if err := os.Chmod(filepath.Join(root, "sandbox_read.sh"), 0o700); err != nil {
		t.Fatal(err)
	}

	copyFile(t, root, filepath.Join("testdata", "sandbox_write.sh"))
	copyFile(t,
		root,
		filepath.Join("testdata", "ctx-os-exec-filesystem_sandbox.star"))

	want := "[//ctx-os-exec-filesystem_sandbox.star:17] sandbox_read.sh retcode: 1\n" +
		"[//ctx-os-exec-filesystem_sandbox.star:18] cat: " + fileOutsideRoot + ": No such file or directory\n" +
		"\n" +
		"[//ctx-os-exec-filesystem_sandbox.star:21] sandbox_write.sh retcode: 1\n" +
		"[//ctx-os-exec-filesystem_sandbox.star:22] touch: cannot touch 'file.txt': Read-only file system\n\n"
	testStarlarkPrint(t, root, "ctx-os-exec-filesystem_sandbox.star", false, false, want)
}

func TestRun_Vendored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	copyTree(t, dir, "testdata/vendored", nil)
	testStarlarkPrint(t, dir, "shac.star", false, false, "[//shac.star:17] True\n")
}

// Utilities

// testStarlarkPrint test a starlark file that calls print().
func testStarlarkPrint(t testing.TB, root, name string, all bool, ignoreOrder bool, want string, files ...string) {
	r := reportPrint{reportNoPrint: reportNoPrint{t: t}}
	o := Options{Report: &r, Dir: root, AllFiles: all, EntryPoint: name, Files: files}
	if err := Run(context.Background(), &o); err != nil {
		t.Helper()
		t.Fatal(err)
	}
	got := r.b.String()
	if ignoreOrder {
		want = sortLines(want)
		got = sortLines(got)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Helper()
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func sortLines(str string) string {
	str = strings.TrimSuffix(str, "\n")
	lines := strings.Split(str, "\n")
	slices.Sort(lines)
	return strings.Join(lines, "\n") + "\n"
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
	dest := t.TempDir()
	copyTree(t, dest, p, nil)
	return dest, out
}

func makeGit(t testing.TB) string {
	t.Helper()
	// scm.go requires two commits. Not really worth fixing yet, it's only
	// annoying in unit tests.
	root := resolvedTempDir(t)
	initGit(t, root)

	writeFile(t, root, "file.txt", "First file\nIt doesn't contain\na lot of lines.\n")
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-m", "Initial commit")

	runGit(t, root, "mv", "file.txt", "a.txt")
	writeFile(t, root, "a.txt", "First file\nIt contains\na lot of lines.\n")
	runGit(t, root, "add", "a.txt")
	writeFile(t, root, "z.txt", "Second file")
	runGit(t, root, "add", "z.txt")
	runGit(t, root, "commit", "-m", "Second commit")
	return root
}

func initGit(t testing.TB, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "engine test")
}

func copySCM(t testing.TB, dst string) {
	m, err := filepath.Glob(filepath.Join("testdata", "scm", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range m {
		copyFile(t, dst, src)
	}
}

// resolvedTempDir creates a new test tempdir and resolves all symlinks. This is
// useful when creating a temp dir and passing into shac, then comparing some
// output, because shac internally resolves symlinks.
func resolvedTempDir(t testing.TB) string {
	d := t.TempDir()
	// Evaluate symlinks so that tests work on Mac, where t.TempDir() returns
	// path under /var, but /var is symlinked to /private/var and os.Getwd()
	// returns the path with symlinks resolved.
	d, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func copyFile(t testing.TB, dst, src string) {
	t.Helper()
	d, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	s, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	writeFileBytes(t, dst, filepath.Base(src), d, s.Mode()&0o700)
}

func writeFile(t testing.TB, root, path, content string) {
	t.Helper()
	writeFileBytes(t, root, path, []byte(content), 0o600)
}

func writeFileBytes(t testing.TB, root, path string, content []byte, perm os.FileMode) {
	t.Helper()
	abs := filepath.Join(root, path)
	mkdirAll(t, filepath.Dir(abs))
	if err := os.WriteFile(abs, content, perm); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runGit(t testing.TB, root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		// Make git versions before 2.32 skip home configuration.
		"GIT_CONFIG_NOGLOBAL=true",
		"HOME=",
		"GIT_CONFIG_NOSYSTEM=true",
		"XDG_CONFIG_HOME=",
		// The right way for more recent git versions to skip home configuration.
		"GIT_CONFIG_GLOBAL=",
		"GIT_CONFIG_SYSTEM=",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00",
		"LANG=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Helper()
		t.Fatalf("failed to run git %s\n%s\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

type reportNoPrint struct {
	t testing.TB
}

func (r *reportNoPrint) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	r.t.Errorf("unexpected finding: %s: %s, %q, %s, %s, %# v, %v", check, level, message, root, file, s, replacements)
	return errors.New("not implemented")
}

func (r *reportNoPrint) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	r.t.Errorf("unexpected artifact: %s: %s", check, file)
	return errors.New("not implemented")
}

func (r *reportNoPrint) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, l Level, err error) {
}

func (r *reportNoPrint) Print(ctx context.Context, check, file string, line int, message string) {
	r.t.Errorf("unexpected print: %s %s(%d): %s", check, file, line, message)
}

type reportPrint struct {
	reportNoPrint
	mu sync.Mutex
	b  strings.Builder
}

func (r *reportPrint) Print(ctx context.Context, check, file string, line int, message string) {
	r.mu.Lock()
	fmt.Fprintf(&r.b, "[%s:%d] %s\n", file, line, message)
	r.mu.Unlock()
}

type finding struct {
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
	mu        sync.Mutex
	findings  []finding
	artifacts []artifact
}

func (r *reportEmitNoPrint) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	r.mu.Lock()
	r.findings = append(r.findings, finding{
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
	findings  []finding
	artifacts []artifact
}

func (r *reportEmitPrint) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	r.mu.Lock()
	r.findings = append(r.findings, finding{
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
