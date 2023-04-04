// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate go run docregen_stdlib.go

package engine

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.chromium.org/luci/lucicfg/docgen"
	"go.chromium.org/luci/lucicfg/docgen/ast"
	"go.fuchsia.dev/shac-project/shac/doc"
)

//go:embed docgen.mdt
var docgenTpl string

// Doc returns the documentation for a source file.
func Doc(src string) (string, error) {
	content := ""
	if src == "stdlib" {
		src = "stdlib.star"
		content = doc.StdlibSrc
	} else {
		if !strings.HasSuffix(src, ".star") {
			return "", errors.New("invalid source file name, expecting .star suffix")
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return "", err
		}
		content = string(b)
	}
	return genDoc(src, content)
}

func genDoc(src, content string) (string, error) {
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

	g := docgen.Generator{
		Starlark: func(m string) (string, error) {
			// 'module' here is something like "@stdlib//path".
			if m != src {
				return "", fmt.Errorf("unknown module %q", m)
			}
			return content, nil
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
			if name == "load_" {
				name = "load"
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
