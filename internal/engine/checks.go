// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path"
	"strings"

	"go.chromium.org/luci/common/errors"
	"go.chromium.org/luci/lucicfg/docgen"
	"go.chromium.org/luci/starlark/builtins"
	"go.chromium.org/luci/starlark/interpreter"
	"go.fuchsia.dev/shac-project/shac/doc"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

//go:embed stdlib.mdt
var shacMDTemplate string

// checks is a list of registered checks callbacks.
//
// It lives in state. Checks are executed sequentially after all Starlark
// code is loaded. They run checks and emit results (results and comments).
type checks struct {
	c []starlark.Callable
}

// add registers a new callback.
func (c *checks) add(cb starlark.Callable) error {
	c.c = append(c.c, cb)
	return nil
}

// callAll calls all the checks.
func (c *checks) callAll(ctx context.Context, th *starlark.Thread) errors.MultiError {
	// TODO(maruel): Require go1.20 and use the new stdlib native multierror
	// support.
	var errs errors.MultiError
	fc := builtins.GetFailureCollector(th)
	args := starlark.Tuple{getShac()}
	args.Freeze()
	for _, cb := range c.c {
		if fc != nil {
			fc.Clear()
		}
		if _, err := starlark.Call(th, cb, args, nil); err != nil {
			if fc != nil && fc.LatestFailure() != nil {
				// Prefer this error, it has custom stack trace.
				errs = append(errs, fc.LatestFailure())
			} else {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// getShac returns the shac object.
//
// Make sure to update stdlib.star whenever this object is modified.
func getShac() starlark.Value {
	return toValue("shac", starlark.StringDict{
		"exec": builtins.Fail,
		"io": toValue("io", starlark.StringDict{
			"read_file": starlark.NewBuiltin("read_file", readFile),
		}),
		"re": toValue("re", starlark.StringDict{
			"match":      starlark.NewBuiltin("match", reMatch),
			"allmatches": starlark.NewBuiltin("allmatches", reAllMatches),
		}),
		"result": toValue("result", starlark.StringDict{
			"emit_comment":  builtins.Fail,
			"emit_row":      builtins.Fail,
			"emit_artifact": builtins.Fail,
		}),
		"scm": toValue("scm", starlark.StringDict{
			"affected_files": starlark.NewBuiltin("affected_files", scmAffectedFiles),
			"all_files":      starlark.NewBuiltin("all_files", scmAllFiles),
		}),
	})
}

// getDoc returns documentation for all the interfaces exposed by shac.
func getDoc() string {
	g := docgen.Generator{
		Starlark: func(m string) (string, error) {
			// 'module' here is something like "@stdlib//path".
			if m != "main.star" {
				return "", fmt.Errorf("unknown module %q", m)
			}
			return doc.StdlibSrc, nil
		},
	}
	b, err := g.Render(shacMDTemplate)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// toValue converts a StringDict to a Value.
func toValue(name string, d starlark.StringDict) starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String(name), d)
}

// registerCheck implements native function register_check().
//
// Make sure to update stdlib.star whenever this function is modified.
func registerCheck(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cb starlark.Callable
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &cb); err != nil {
		return nil, err
	}
	if len(kwargs) != 0 {
		return nil, errors.New("unexpected arguments")
	}
	// TODO(maruel): Inspect cb to verify that it accepts one argument.
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	if s.doneLoading {
		return nil, errors.New("can't register checks after done loading")
	}
	return starlark.None, s.checks.add(cb)
}

// readFile implements native function shac.io.read_file().
//
// Use POSIX style relative path. "..", "\" and absolute paths are denied.
//
// Make sure to update stdlib.star whenever this function is modified.
func readFile(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argname starlark.String
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &argname); err != nil {
		return nil, err
	}
	p := string(argname)
	if strings.Contains(p, "\\") {
		return starlark.None, errors.New("use POSIX style path")
	}
	// Package path use POSIX style even on Windows, unlike path/filepath.
	if path.IsAbs(p) {
		return starlark.None, errors.New("do not use absolute path")
	}
	// This is overly zealous. Revisit if it is too much.
	// TODO(maruel): Make it work on Windows.
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	if path.Clean(p) != p {
		return starlark.None, errors.New("pass cleaned path")
	}
	dst := path.Join(s.inputs.root, p)
	if !strings.HasPrefix(dst, s.inputs.root) {
		return starlark.None, errors.New("cannot escape root")
	}
	//#nosec G304
	b, err := os.ReadFile(dst)
	if err != nil {
		return starlark.None, err
	}
	// TODO(maruel): Use unsafe conversion to save a memory copy.
	return starlark.Bytes(b), nil
}
