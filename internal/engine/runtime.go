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

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.chromium.org/luci/starlark/builtins"
	"go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// getPredeclared returns the predeclared starlark symbols in the runtime.
//
// The upstream starlark interpreter includes all the symbols described at
// https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#built-in-constants-and-functions
// See https://pkg.go.dev/go.starlark.net/starlark#Universe for the default list.
func getPredeclared() starlark.StringDict {
	return starlark.StringDict{
		"shac": toValue("shac", getShac()),

		// Add https://bazel.build/rules/lib/json so it feels more natural to bazel
		// users.
		"json": json.Module,

		// Override fail to include additional functionality.
		//
		// Do not use newBuiltinNone() because it needs access to the thread to
		// capture the stack trace.
		"fail": starlark.NewBuiltin("fail", fail),
		// struct is an helper function that enables users to create seamless
		// object instances.
		"struct": builtins.Struct,
	}
}

// fail aborts execution. When run within a check, associates the check with an "abnormal failure".
//
// Unlike builtins.Fail(), it doesn't allow user specified stack traces.
func fail(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	sep := " "
	// Do not exit early if the arguments are wrong.
	err := starlark.UnpackArgs(fn.Name(), nil, kwargs, "sep?", &sep)
	buf := strings.Builder{}
	for i, v := range args {
		if i > 0 {
			buf.WriteString(sep)
		}
		if s, ok := starlark.AsString(v); ok {
			buf.WriteString(s)
		} else {
			buf.WriteString(v.String())
		}
	}
	if err != nil {
		buf.WriteString("\n")
		buf.WriteString(err.Error())
	}
	msg := buf.String()
	failErr := &failure{
		Message: msg,
		Stack:   th.CallStack(),
	}
	ctx := getContext(th)
	if c := ctxCheck(ctx); c != nil {
		// Running inside a check, annotate it.
		c.failErr = failErr
	} else {
		// Save the error in the shacState object since we are in the first phase.
		s := ctxShacState(ctx)
		s.failErr = failErr
	}
	return nil, errors.New(fn.Name() + ": " + msg)
}

// toValue converts a StringDict to a Value.
func toValue(name string, d starlark.StringDict) starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String(name), d)
}

type builtin func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)

// newBuiltin registers a go function as a Starlark builtin.
//
// It's identical to `starlark.NewBuiltin()`, but prepends the function name to
// the text of any returned errors as a usability improvement.
func newBuiltin(name string, impl builtin) *starlark.Builtin {
	wrapper := func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		ctx := getContext(th)
		s := ctxShacState(ctx)
		val, err := impl(ctx, s, name, args, kwargs)
		// starlark.UnpackArgs already adds the function name prefix to errors
		// it returns, so make sure not to duplicate the prefix if it's already
		// there.
		if err != nil && !strings.HasPrefix(err.Error(), name+": ") {
			err = fmt.Errorf("%s: %w", name, err)
		}
		if val != nil {
			// All values returned by builtins are immutable. This is not a hard
			// requirement, and can be relaxed if there's a use case for mutable
			// return values, but it's still a sensible default.
			val.Freeze()
		}
		return val, err
	}
	return starlark.NewBuiltin(name, wrapper)
}

func newBuiltinNone(name string, f func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error) *starlark.Builtin {
	return newBuiltin(
		name,
		func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, f(ctx, s, name, args, kwargs)
		})
}
