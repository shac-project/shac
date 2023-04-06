// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"errors"
	"runtime/debug"

	"go.chromium.org/luci/starlark/builtins"
	"go.chromium.org/luci/starlark/interpreter"
	"go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// getPredeclared returns the predeclared starlark symbols in the runtime.
func getPredeclared() starlark.StringDict {
	// The upstream starlark interpreter includes all the symbols described at
	// https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#built-in-constants-and-functions
	// See https://pkg.go.dev/go.starlark.net/starlark#Universe for the default list.
	return starlark.StringDict{
		"shac": toValue("shac", starlark.StringDict{
			"register_check": starlark.NewBuiltin("register_check", registerCheck),
			"commit_hash":    starlark.String(getCommitHash()),
			"version": starlark.Tuple{
				starlark.MakeInt(version[0]), starlark.MakeInt(version[1]), starlark.MakeInt(version[2]),
			},
		}),

		// Add https://bazel.build/rules/lib/json so it feels more natural to bazel
		// users.
		"json": json.Module,

		// Override fail to include additional functionality.
		"fail": builtins.Fail,
		// struct is an helper function that enables users to create seamless
		// object instances.
		"struct": builtins.Struct,
	}
}

// registerCheck implements native function shac.register_check().
//
// Make sure to update stdlib.star whenever this function is modified.
func registerCheck(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cb starlark.Callable
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"cb", &cb,
	); err != nil {
		return nil, err
	}
	// TODO(maruel): Inspect cb to verify that it accepts one argument.
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	if s.doneLoading {
		return nil, errors.New("can't register checks after done loading")
	}
	return starlark.None, s.checks.add(cb)
}

// getCommitHash return the git commit hash that was used to build this
// executable.
func getCommitHash() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

// toValue converts a StringDict to a Value.
func toValue(name string, d starlark.StringDict) starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String(name), d)
}
