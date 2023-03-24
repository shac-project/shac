// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"go.chromium.org/luci/common/data/stringset"
	"go.chromium.org/luci/common/errors"
	"go.chromium.org/luci/starlark/builtins"
	"go.chromium.org/luci/starlark/interpreter"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Load loads a main shac.star file from a root directory.
//
// main is normally shac.star.
func Load(ctx context.Context, root, main string) error {
	_, err := parse(ctx, &inputs{
		code: interpreter.FileSystemLoader(root),
		main: main,
	})
	return err
}

// inputs represents a starlark package.
type inputs struct {
	code interpreter.Loader
	main string
}

// state represents a parsing state of the main starlark tree.
type state struct {
	intr   *interpreter.Interpreter
	inputs *inputs
}

// mergeErrs returns a list of merged errors as a MultiError, deduplicating
// errors with the same backtrace.
func mergeErrs(err ...error) error {
	var errs errors.MultiError
	seenErrs := stringset.New(len(err))
	for _, e := range err {
		if bt, _ := e.(BacktracableError); bt == nil || seenErrs.Add(bt.Backtrace()) {
			errs = append(errs, e)
		}
	}
	return errs
}

const stateCtxKey = "shac.State"

var (
	// stderrPrint is where print() calls are sent.
	stderrPrint io.Writer = os.Stderr
	// version is the current tool version.
	version = [...]int{0, 0, 1}
)

func parse(ctx context.Context, inputs *inputs) (*state, error) {
	failures := builtins.FailureCollector{}
	s := &state{
		intr: &interpreter.Interpreter{
			Predeclared: getPredeclared(),
			Packages: map[string]interpreter.Loader{
				interpreter.MainPkg: inputs.code,
			},
			Logger: func(file string, line int, message string) {
				fmt.Fprintf(stderrPrint, "[%s:%d] %s\n", file, line, message)
			},
			ThreadModifier: func(th *starlark.Thread) {
				failures.Install(th)
			},
		},
		inputs: inputs,
	}
	ctx = context.WithValue(ctx, stateCtxKey, s)
	var err error
	if err = s.intr.Init(ctx); err == nil {
		_, err = s.intr.ExecModule(ctx, interpreter.MainPkg, s.inputs.main)
	}
	if err != nil {
		if f := failures.LatestFailure(); f != nil {
			// Prefer the collected error if any, it will have a collected trace.
			err = f
		}
		return nil, mergeErrs(err)
	}
	// TODO(maruel): Error if there are unconsumed variables once variables are
	// added.
	return s, nil
}

// getPredeclared returns the predeclared starlark symbols in the runtime.
func getPredeclared() starlark.StringDict {
	// TODO(maruel): Add more native symbols.
	native := starlark.StringDict{
		"commitHash": starlark.String(getCommitHash()),
		// TODO(maruel): Add proper version, preferably from git tag.
		"version": starlark.Tuple{
			starlark.MakeInt(version[0]), starlark.MakeInt(version[1]), starlark.MakeInt(version[2]),
		},
	}
	return starlark.StringDict{
		"__native__": starlarkstruct.FromStringDict(starlark.String("__native__"), native),
	}
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
