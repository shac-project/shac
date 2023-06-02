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

	"go.starlark.net/starlark"
)

// shacCheck implements native function shac.check().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func shacCheck(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argimpl *starlark.Function
	var argname starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"impl", &argimpl,
		"name?", &argname); err != nil {
		return nil, err
	}
	return newCheck(argimpl, string(argname))
}

func newCheck(impl starlark.Callable, name string) (*check, error) {
	if _, ok := impl.(*starlark.Builtin); ok {
		return nil, errors.New("\"impl\" must not be a built-in function")
	}
	fun, ok := impl.(*starlark.Function)
	if !ok || fun.NumParams() == 0 {
		return nil, errors.New("\"impl\" must be a function accepting one \"ctx\" argument")
	}
	if ctxParam, _ := fun.Param(0); ctxParam != "ctx" {
		return nil, errors.New("\"impl\"'s first parameter must be named \"ctx\"")
	}
	if fun.ParamDefault(0) != nil {
		return nil, errors.New("\"impl\" must not have a default value for the \"ctx\" parameter")
	}
	for i := 1; i < fun.NumParams(); i++ {
		if fun.ParamDefault(i) == nil {
			return nil, errors.New("\"impl\" can only have one required argument")
		}
	}
	if name == "" {
		if fun.Name() == "lambda" {
			return nil, errors.New("\"name\" must be set when \"impl\" is a lambda")
		}
		name = strings.TrimPrefix(fun.Name(), "_")
	}
	return &check{impl: fun, name: name}, nil
}

// check represents a runnable shac check as returned by shac.check().
type check struct {
	impl *starlark.Function
	name string
}

var _ starlark.Value = (*check)(nil)

func (c *check) String() string {
	return fmt.Sprintf("<check %s>", c.name)
}

func (c *check) Type() string {
	return "shac.check"
}

func (c *check) Truth() starlark.Bool {
	return true
}

func (c *check) Freeze() {
	c.impl.Freeze()
}

func (c *check) Hash() (uint32, error) {
	// starlark.Function.Hash() returns the hash of the function name, so
	// hashing just the name of the check seems reasonable.
	return starlark.String(c.name).Hash()
}
