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

// Package main implements a check that the os/exec.Cmd Start() and Run()
// functions aren't called directly.
//
// Instead, callers should use the execsupport package.
package main

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"
)

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			funcName := selector.Sel.Name
			if funcName != "Start" && funcName != "Run" {
				return true
			}

			var obj *ast.Ident
			switch o := selector.X.(type) {
			case *ast.Ident:
				obj = o
			case *ast.SelectorExpr:
				obj = o.Sel
			default:
				return true
			}

			// Skip function calls that aren't struct methods.
			if obj == nil {
				return true
			}

			typ := pass.TypesInfo.ObjectOf(obj).Type()
			if typ.String() == "*os/exec.Cmd" {
				pass.Reportf(n.Pos(), "do not call %s.%s() directly, use execsupport.%s(%s) instead",
					obj.Name, funcName, funcName, obj.Name)
			}
			return false
		})
	}
	return nil, nil
}

func main() {
	multichecker.Main(
		&analysis.Analyzer{
			Name: "directexec",
			Doc:  "do not call os/exec.Cmd Start or Run functions directly",
			Run:  run,
		},
	)
}
