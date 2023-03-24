// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"

	"go.chromium.org/luci/common/errors"
	"go.chromium.org/luci/starlark/builtins"
	"go.chromium.org/luci/starlark/interpreter"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

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

func getShac() starlark.Value {
	return toValue("shac", starlark.StringDict{
		"exec": builtins.Fail,
		"io": toValue("io", starlark.StringDict{
			"read_file": builtins.Fail,
		}),
		"result": toValue("result", starlark.StringDict{
			"emit_comment":  builtins.Fail,
			"emit_row":      builtins.Fail,
			"emit_artifact": builtins.Fail,
		}),
	})
}

// toValue converts a StringDict to a Value.
func toValue(name string, d starlark.StringDict) starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String(name), d)
}

var registerCheck = starlark.NewBuiltin("register_check", func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
})
