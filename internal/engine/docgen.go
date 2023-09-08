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

//go:generate go run docregen_stdlib.go

package engine

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.chromium.org/luci/starlark/docgen"
	"go.chromium.org/luci/starlark/docgen/ast"
	"go.fuchsia.dev/shac-project/shac/doc"
	"google.golang.org/protobuf/encoding/prototext"
)

//go:embed docgen.mdt
var docgenTpl string

// Doc returns the documentation for a source file.
//
// src must be either a path to a source file or the string "stdlib".
func Doc(src string) (string, error) {
	content := ""
	isStdlib := false
	if src == "stdlib" {
		isStdlib = true
		src = "stdlib.star"
		content = doc.StdlibSrc
	} else {
		if strings.HasPrefix(src, "@") {
			return "", errors.New("todo: implement @module")
		}
		if !strings.HasSuffix(src, ".star") {
			return "", errors.New("invalid source file name, expecting .star suffix")
		}
		var err error
		if src, err = filepath.Abs(src); err != nil {
			return "", err
		}
		b, err := os.ReadFile(src)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("file %s not found", src)
			}
			return "", err
		}
		content = string(b)
	}
	tmpdir, err := os.MkdirTemp("", "shac")
	if err != nil {
		return "", err
	}
	d, err := genDoc(tmpdir, src, content, isStdlib)
	if err2 := os.RemoveAll(tmpdir); err == nil {
		err = err2
	}
	return d, err
}

func genDoc(tmpdir, src, content string, isStdlib bool) (string, error) {
	// It's unfortunate that we parse the source file twice. We need to fix the
	// upstream API.
	m, err := ast.ParseModule(src, content, func(s string) (string, error) { return s, nil })
	if err != nil {
		return "", err
	}

	// Parse once to get all the global symbols and top level docstring.
	var syms []ast.Node
	for _, node := range m.Nodes {
		if !strings.HasPrefix(node.Name(), "_") {
			syms = append(syms, node)
		}
	}
	// Load packages to get the exported symbols.
	pkgMgr := NewPackageManager(tmpdir)
	var packages map[string]fs.FS
	root := filepath.Dir(src)
	if !isStdlib {
		var b []byte
		if b, err = os.ReadFile(filepath.Join(root, "shac.textproto")); err == nil {
			doc := Document{}
			if err = prototext.Unmarshal(b, &doc); err != nil {
				return "", err
			}
			if err = doc.Validate(); err != nil {
				return "", err
			}
			if packages, err = pkgMgr.RetrievePackages(context.Background(), root, &doc); err != nil {
				return "", err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		} else {
			// Still allow local access even if no shac.textproto is present.
			packages = map[string]fs.FS{"__main__": os.DirFS(root)}
		}
	}
	d := m.Doc()
	parent := sourceKey{pkg: "__main__", relpath: src}
	g := docgen.Generator{
		Normalize: func(p, s string) (string, error) {
			return normalize(parent, p, s)
		},
		Starlark: func(m string) (string, error) {
			if m == src {
				return content, nil
			}
			return getStarlark(packages, m)
		},
	}

	// Appends all the global symbols to the template to render them.
	gen := ""
	// First, "load" the symbols.
	for i, n := range syms {
		gen += fmt.Sprintf("{{- $sym%d := Symbol %q %q }}", i, src, n.Name())
	}

	// Header and main comment if any.
	if len(d) != 0 {
		// If a module has a docstring, use the first line as the header.
		gen += "# " + strings.TrimSpace(d)
	} else {
		// TODO(maruel): Maybe the absolute path? Or a module docstring?
		gen += "# " + src
	}

	// Generate the table of content.
	if len(syms) != 0 {
		gen += "\n\n## Table of contents\n\n"
		// TODO(maruel): Use "{{ template \"gen-toc\" }}"
		for _, n := range syms {
			name := n.Name()
			if isStdlib {
				// To avoid overriding stdlib built-in functions, their
				// docstrings are attached to dummy functions of the same name
				// but with a trailing underscore.
				name = strings.TrimSuffix(name, "_")
			}
			// Anchor works here because top-level symbols are generally simple. It
			// is brittle, especially with the different anchor generation algorithm
			// between GitHub and Gitiles.
			gen += "- [" + name + "](#" + name + ")\n"
		}
	}

	// Each of the symbol.
	for i := range syms {
		gen += fmt.Sprintf("\n{{ template \"gen-any\" $sym%d}}\n", i)
	}
	b, err := g.Render(docgenTpl + gen)
	return string(b), err
}

func normalize(parent sourceKey, p, s string) (string, error) {
	skp, err := parseSourceKey(parent, p)
	if err != nil {
		return "", err
	}
	sks, err := parseSourceKey(skp, s)
	return sks.String(), err
}

func getStarlark(packages map[string]fs.FS, m string) (string, error) {
	pkg := "__main__"
	relpath := ""
	if strings.HasPrefix(m, "@") {
		parts := strings.SplitN(m[1:], "//", 2)
		pkg = parts[0]
		relpath = parts[1]
	} else if strings.HasPrefix(m, "//") {
		relpath = m[2:]
	}
	ref := packages[pkg]
	if ref == nil {
		return "", fmt.Errorf("package %s not found", pkg)
	}
	d, err := ref.Open(relpath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("file not found")
	}
	if err != nil {
		return "", err
	}
	b, err := io.ReadAll(d)
	if err2 := d.Close(); err == nil {
		err = err2
	}
	return string(b), err
}
