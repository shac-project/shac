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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.chromium.org/luci/starlark/docgen"
)

func TestDocStdlib(t *testing.T) {
	// This is a state change detector.
	t.Parallel()
	got, err := Doc("stdlib")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "doc", "stdlib.md"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(string(b), got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestDoc(t *testing.T) {
	t.Parallel()
	p, err := filepath.Abs("../../shac.star")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Doc(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# Checks for shac itself\n\n") {
		t.Fatal(got)
	}
}

func TestDoc_Error(t *testing.T) {
	t.Parallel()
	data := []struct {
		path string
		err  string
	}{
		{
			"file",
			"invalid source file name, expecting .star suffix",
		},
		{
			"@remote//file.star",
			"todo: implement @module",
		},
		{
			"/non_existent.star",
			func() string {
				if runtime.GOOS == "windows" {
					p, err := filepath.Abs("/non_existent.star")
					if err != nil {
						t.Fatal(err)
					}
					return "file " + p + " not found"
				}
				return "file /non_existent.star not found"
			}(),
		},
	}
	for i, line := range data {
		line := line
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			d, err := Doc(line.path)
			if err == nil {
				t.Fatalf("expected error, got: %s", d)
			}
			got := err.Error()
			if diff := cmp.Diff(line.err, got); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDocTemplate(t *testing.T) {
	t.Parallel()
	data := []struct {
		in   string
		want string
	}{
		{
			"def foo():\n  pass\n",
			"# main.star\n\n## Table of contents\n\n- [foo](#foo)\n\n## foo\n",
		},
		{
			"\n",
			// TODO(maruel): Unexpected LF placement.
			"\n# main.star",
		},
		{
			"foo = True\n",
			// TODO(maruel): Too many trailing spaces.
			// TODO(maruel): A global shouldn't be considered an unknown. There's
			// probably a parsing error.
			"# main.star\n\n## Table of contents\n\n- [foo](#foo)\n\n\nUnknown.\n",
		},
		{
			"foo = struct()\n",
			// TODO(maruel): Too many trailing spaces.
			"# main.star\n\n## Table of contents\n\n- [foo](#foo)\n\n## foo\n\n\n",
		},
	}
	for i, line := range data {
		line := line
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			// This test case cannot call load() because "main.star" is not an absolute path.
			got, err := genDoc(t.TempDir(), "main.star", line.in, false)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(line.want, got); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDocTemplate_Testdata_Err(t *testing.T) {
	t.Parallel()
	data := []struct {
		path string
		err  string
	}{
		{
			"testdata/docgen/err/missing_local/test.star",
			`template: main:115:13: executing "main" at <Symbol "testdata/docgen/err/missing_local/test.star" "fn">: error calling Symbol: in testdata/docgen/err/missing_local/test.star: in //non_existent.star: file not found`,
		},
		{
			"testdata/docgen/err/missing_pkg/test.star",
			`template: main:115:13: executing "main" at <Symbol "testdata/docgen/err/missing_pkg/test.star" "fn">: error calling Symbol: in testdata/docgen/err/missing_pkg/test.star: in @remote//non_existent.star: package remote not found`,
		},
		{
			"testdata/docgen/err/textproto_is_dir/test.star",
			func() string {
				if runtime.GOOS == "windows" {
					return "read testdata\\docgen\\err\\textproto_is_dir\\shac.textproto: Incorrect function."
				}
				return "read testdata/docgen/err/textproto_is_dir/shac.textproto: is a directory"
			}(),
		},
	}
	for i, line := range data {
		line := line
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			in, err := os.ReadFile(line.path)
			if err != nil {
				t.Fatal(err)
			}
			d, err := genDoc(filepath.Dir(line.path), line.path, string(in), false)
			if err == nil {
				t.Fatalf("expected error, got: %s", d)
			}
			got := err.Error()
			if diff := cmp.Diff(line.err, got); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDocTemplateLoad_Load_Local(t *testing.T) {
	t.Parallel()
	f := "testdata/docgen/load_local/test.star"
	in, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := genDoc(filepath.Dir(f), f, string(in), false)
	if err != nil {
		t.Fatal(err)
	}
	want := "# testdata/docgen/load_local/test.star\n\n## Table of contents\n\n- [fn](#fn)\n- [sym](#sym)\n\n## fn\n\nDo stuff.\n\n\n## sym\n\n\n\nFields:\n\n- fn\n\n## sym.fn\n\nDo stuff.\n\n"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestDocgenGenerator(t *testing.T) {
	// It's not really a unit test, it's more to document parts of what is
	// available in the template engine.
	t.Parallel()
	src := `
# This is a comment.
def _affected_files():
  """Returns affected files.

  When --all is passed, all files are returned.

  Args:
    glob: A glob to filter returned filed.

  Returns:
    A map of {path: file(...)} where the struct has a string field action and a
    function new_line().
  """
  pass

ctx = struct(
  scm = struct(
    affected_files = _affected_files,
  ),
)

file = struct(
  action = "A",
)
`
	common := `{{ $af := Symbol "main.star" "ctx.scm.affected_files" }}` +
		`{{- $file := Symbol "main.star" "file" }}` +
		`{{- $ctx := Symbol "main.star" "ctx" }}`
	data := []struct {
		in   string
		want string
	}{
		{"{{ $af.Name }}", "affected_files"},
		{"{{ $af.Def.Name }}", "_affected_files"},
		{"{{ $af.Def.Doc }}",
			// The original unparsed docstring as-is.
			"Returns affected files.\n\n" +
				"  When --all is passed, all files are returned.\n\n" +
				"  Args:\n" +
				"    glob: A glob to filter returned filed.\n\n" +
				"  Returns:\n" +
				"    A map of {path: file(...)} where the struct has a string field action and a\n" +
				"    function new_line().\n" +
				"  ",
		},
		{"{{ $af.Def.Comments }}", "This is a comment."},
		{"{{ $af.Doc.Description }}",
			"Returns affected files.\n\n" +
				"When --all is passed, all files are returned.",
		},
		{"{{ $af.Doc.Fields }}", "[{Args [{glob A glob to filter returned filed.}]}]"},
		{"{{ $af.Doc.Remarks }}",
			"[{Returns A map of {path: file(...)} where the struct has a string field action and a\n" +
				"function new_line().}]",
		},
		{"{{ $af.Doc.Args }}", "[{glob A glob to filter returned filed.}]"},
		{"{{ $af.Doc.Returns }}",
			"A map of {path: file(...)} where the struct has a string field action and a\n" +
				"function new_line().",
		},
		{"{{ $af.Doc.Returns | LinkifySymbols }}",
			// TODO(maruel): file(...) should have become a link.
			"A map of {path: file(...)} where the struct has a string field action and a\n" +
				"function new_line().",
		},
		{"{{ $af.Module }}", "main.star"},
		{"{{ $af.FullName }}", "ctx.scm.affected_files"},
		{"{{ $af.Anchor }}", "ctx.scm.affected-files"},
		{"{{ $af.Flavor }}", "func"},
		{"{{ $af.InvocationSnippet }}", "ctx.scm.affected_files(glob = None)"},
		{"{{ $ctx.Flavor }}", "struct"},
		{"{{ range $ctx.Symbols }}{{ .Name }}\n{{ end }}", "scm\n"},
	}
	for i, line := range data {
		line := line
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			g := docgen.Generator{
				Starlark: func(m string) (string, error) {
					if m != "main.star" {
						t.Fatal(m)
					}
					// See https://pkg.go.dev/go.chromium.org/luci/lucicfg/docgen/docstring for
					// the parsing algorithm.
					return src, nil
				},
			}
			b, err := g.Render(common + line.in)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(line.want, string(b)); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s\n%s", line.in, diff)
			}
		})
	}
}
