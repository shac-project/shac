// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"errors"
	"runtime/debug"
	"strings"

	"go.chromium.org/luci/starlark/builtins"
	"go.chromium.org/luci/starlark/interpreter"
	"go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

var (
	// version is the current tool version.
	//
	// TODO(maruel): Add proper version, preferably from git tag.
	version = [...]int{0, 0, 1}
)

// getPredeclared returns the predeclared starlark symbols in the runtime.
//
// The upstream starlark interpreter includes all the symbols described at
// https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#built-in-constants-and-functions
// See https://pkg.go.dev/go.starlark.net/starlark#Universe for the default list.
func getPredeclared() starlark.StringDict {
	return starlark.StringDict{
		"shac": toValue("shac", starlark.StringDict{
			"register_check": starlark.NewBuiltin("register_check", shacRegisterCheck),
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

// shacRegisterCheck implements native function shac.register_check().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func shacRegisterCheck(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cb starlark.Callable
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"callback", &cb,
	); err != nil {
		return nil, err
	}
	// TODO(maruel): Inspect cb to verify that it accepts one argument.
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	if s.doneLoading {
		return nil, errors.New("can't register checks after done loading")
	}
	// Register the new callback.
	s.checks = append(s.checks, check{cb: cb, name: strings.TrimPrefix(cb.Name(), "_")})
	return starlark.None, nil
}

// getCommitHash return the git commit hash that was used to build this
// executable.
//
// Since shac is currently tracked in a git repository and git currently uses
// SHA-1, it is a 40 characters hex encoded string.
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
