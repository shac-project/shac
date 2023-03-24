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
	var errs errors.MultiError
	fc := builtins.GetFailureCollector(th)
	for _, cb := range c.c {
		if fc != nil {
			fc.Clear()
		}
		// TODO(maruel): Pass the shac argument.
		if _, err := starlark.Call(th, cb, starlark.Tuple{}, nil); err != nil {
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

var registerCheck = starlark.NewBuiltin("register_check", func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cb starlark.Callable
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &cb); err != nil {
		return nil, err
	}
	if len(kwargs) != 0 {
		return nil, errors.New("unexpected arguments")
	}
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	if s.doneLoading {
		return nil, errors.New("can't register checks after done loading")
	}
	return starlark.None, s.checks.add(cb)
})
