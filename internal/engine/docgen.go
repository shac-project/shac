// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate go run docregen_stdlib.go

package engine

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.chromium.org/luci/starlark/docgen"
	"go.chromium.org/luci/starlark/docgen/ast"
	"go.fuchsia.dev/shac-project/shac/doc"
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
			return "", err
		}
		content = string(b)
	}
	return genDoc(src, content, isStdlib)
}

func genDoc(src, content string, isStdlib bool) (string, error) {
	// It's unfortunate that we parse the source file twice. We need to fix the
	// upstream API.
	m, err := ast.ParseModule(src, content)
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
	d := m.Doc()
	root := filepath.Dir(src)
	g := docgen.Generator{
		Starlark: func(m string) (string, error) {
			if m == src {
				return content, nil
			}
			if strings.HasPrefix(m, "@") {
				return "", fmt.Errorf("todo: implement @module; unknown module %q", m)
			}
			// TODO(maruel): Correctly manage "//" prefix.
			m = strings.TrimPrefix(m, "//")
			b, err2 := os.ReadFile(filepath.Join(root, m))
			if err2 != nil {
				return "", fmt.Errorf("failed to load module %q: %w", m, err2)
			}
			return string(b), nil
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
