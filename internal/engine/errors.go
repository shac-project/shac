// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"errors"

	"go.starlark.net/starlark"
)

// ErrCheckFailed is returned by Run() when at least one check failed.
//
// The information will have been provided via the Report interface.
var ErrCheckFailed = errors.New("a check failed")

// BacktracableError is an error that has a starlark backtrace attached to it.
type BacktracableError interface {
	error
	// Backtrace returns a user-friendly error message describing the stack
	// of calls that led to this error, along with the error message itself.
	Backtrace() string
}

// failure is an error emitted by fail(...).
type failure struct {
	Message string             // the error message, as passed to fail(...)
	Stack   starlark.CallStack // where 'fail' itself was called
}

// Error is the short error message, as passed to fail(...).
func (f *failure) Error() string {
	return "fail: " + f.Message
}

// Backtrace returns a user-friendly error message describing the stack of
// calls that led to this error.
//
// The trace of where fail(...) happened is used.
func (f *failure) Backtrace() string {
	c := f.Stack
	if len(c) > 0 && c[len(c)-1].Pos.Filename() == "<builtin>" {
		c = c[:len(c)-1]
	}
	return c.String()
}

// evalError is starlark.EvalError with an optimized Backtrace() function.
type evalError struct {
	*starlark.EvalError
}

// Backtrace returns a user-friendly error message describing the stack
// of calls that led to this error.
func (e *evalError) Backtrace() string {
	c := e.CallStack
	if len(c) > 0 && c[len(c)-1].Pos.Filename() == "<builtin>" {
		c = c[:len(c)-1]
	}
	return c.String()
}

var (
	_ BacktracableError = (*failure)(nil)
	_ BacktracableError = (*evalError)(nil)
)
