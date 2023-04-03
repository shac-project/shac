// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"os"
	"os/exec"
	"path"
	"strings"

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
		"exec": starlark.NewBuiltin("exec", execSubprocess),
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
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	dst, err := absPath(string(argname), s.inputs.root)
	if err != nil {
		return starlark.None, err
	}
	//#nosec G304
	b, err := os.ReadFile(dst)
	if err != nil {
		return starlark.None, err
	}
	// TODO(maruel): Use unsafe conversion to save a memory copy.
	return starlark.Bytes(b), nil
}

// absPath makes a source-relative path absolute, validating it along the way.
//
// TODO(maruel): Make it work on Windows.
func absPath(rel, rootDir string) (string, error) {
	if strings.Contains(rel, "\\") {
		return "", errors.New("use POSIX style path")
	}
	// Package path use POSIX style even on Windows, unlike path/filepath.
	if path.IsAbs(rel) {
		return "", errors.New("do not use absolute path")
	}
	// This is overly zealous. Revisit if it is too much.
	if path.Clean(rel) != rel {
		return "", errors.New("pass cleaned path")
	}
	pathParts := append([]string{rootDir}, strings.Split(rel, "/")...)
	res := path.Join(pathParts...)
	if !strings.HasPrefix(res, rootDir) {
		return "", errors.New("cannot escape root")
	}
	return res, nil
}

// execSubprocess implements the native function shac.exec().
//
// TODO(olivernewman): Return a struct with stdout and stderr in addition to the
// exit code.
//
// Make sure to update stdlib.star whenever this function is modified.
func execSubprocess(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var rawCmd *starlark.List
	var cwd starlark.String
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"cmd", &rawCmd,
		"cwd?", &cwd,
	); err != nil {
		return nil, err
	}
	if rawCmd.Len() == 0 {
		return starlark.None, errors.New("cmdline must not be an empty list")
	}

	var parsedCmd []string
	var val starlark.Value
	iter := rawCmd.Iterate()
	defer iter.Done()
	for iter.Next(&val) {
		str, ok := val.(starlark.String)
		if !ok {
			return starlark.None, errors.New("command args must be strings")
		}
		parsedCmd = append(parsedCmd, str.GoString())
	}

	ctx := interpreter.Context(th)
	s := ctxState(ctx)

	// TODO(olivernewman): Wrap with nsjail on linux.
	//#nosec G204
	cmd := exec.CommandContext(ctx, parsedCmd[0], parsedCmd[1:]...)

	if cwd.GoString() != "" {
		var err error
		cmd.Dir, err = absPath(cwd.GoString(), s.inputs.root)
		if err != nil {
			return starlark.None, err
		}
	} else {
		cmd.Dir = s.inputs.root
	}

	if err := cmd.Run(); err != nil {
		if errExit := (&exec.ExitError{}); errors.As(err, &errExit) {
			return starlark.MakeInt(errExit.ExitCode()), nil
		}
		return starlark.None, err
	}
	return starlark.MakeInt(0), nil
}
