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
	"log"
	"slices"
	"strings"

	"go.starlark.net/starlark"
)

// shacCheck implements native function shac.check().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func shacCheck(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argimpl *starlark.Function
	var argname starlark.String
	var argformatter starlark.Bool
	if err := starlark.UnpackArgs(name, args, kwargs,
		"impl", &argimpl,
		"name?", &argname,
		"formatter?", &argformatter); err != nil {
		return nil, err
	}
	return newCheck(argimpl, string(argname), bool(argformatter))
}

func newCheck(impl starlark.Callable, name string, formatter bool) (*check, error) {
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

	// Checks should not accept arbitrary positional or keyword arguments. This
	// restriction can be reconsidered if there turns out to be a valid use
	// case.
	if fun.HasVarargs() {
		return nil, errors.New("\"impl\" must not accept *args")
	}
	if fun.HasKwargs() {
		return nil, errors.New("\"impl\" must not accept **kwargs")
	}

	// Check impl functions are called internally by shac without any arguments
	// by default, so they will fail if they have any required arguments.
	//
	// It's only necessary to check the first parameter after "ctx" because it's
	// illegal for any required parameters to come after optional ones, so if
	// the first parameter is optional then the rest are as well.
	if fun.NumParams() > 1 && fun.ParamDefault(1) == nil {
		return nil, errors.New("\"impl\" cannot have required arguments besides \"ctx\"")
	}

	if name == "" {
		if fun.Name() == "lambda" {
			return nil, errors.New("\"name\" must be set when \"impl\" is a lambda")
		}
		name = strings.TrimPrefix(fun.Name(), "_")
	}
	return &check{
		impl:      fun,
		name:      name,
		formatter: formatter,
	}, nil
}

// check represents a runnable shac check as returned by shac.check().
type check struct {
	impl *starlark.Function
	name string
	// Whether the check is an auto-formatter or not.
	formatter bool
	kwargs    []starlark.Tuple
}

var _ starlark.HasAttrs = (*check)(nil)

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

func (c *check) Attr(name string) (starlark.Value, error) {
	switch name {
	case "with_args":
		return checkWithArgsBuiltin.BindReceiver(c), nil
	case "with_name":
		return checkWithNameBuiltin.BindReceiver(c), nil
	default:
		return nil, nil
	}
}

func (c *check) AttrNames() []string {
	return []string{"with_args", "with_name"}
}

func (c *check) withName(name string) (starlark.Value, error) {
	// Make a copy to modify.
	res := *c
	res.name = name
	return &res, nil
}

func (c *check) withArgs(kwargs []starlark.Tuple) (starlark.Value, error) {
	// Make a copy to modify.
	res := *c

	validParams := make([]string, 0, c.impl.NumParams()-1)
	for i := 1; i < c.impl.NumParams(); i++ {
		name, _ := c.impl.Param(i)
		validParams = append(validParams, name)
	}

	newKwargs := kwargsMap(res.kwargs)
	for k, v := range kwargsMap(kwargs) {
		if k == "ctx" {
			return nil, errors.New("\"ctx\" argument cannot be overridden")
		}
		if !slices.Contains(validParams, k) {
			return nil, fmt.Errorf("invalid argument %q, must be one of: (%s)", k, strings.Join(validParams, ", "))
		}
		newKwargs[k] = v
	}

	res.kwargs = make([]starlark.Tuple, 0, len(newKwargs))
	for k, v := range newKwargs {
		res.kwargs = append(res.kwargs, starlark.Tuple{starlark.String(k), v})
	}
	return &res, nil
}

// checkWithArgsBuiltin implements the native function shac.check().with_args().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
var checkWithArgsBuiltin = newBoundBuiltin("with_args", func(ctx context.Context, s *shacState, name string, self starlark.Value, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("only keyword arguments are allowed")
	}
	return self.(*check).withArgs(kwargs)
})

// checkWithNameBuiltin implements the native function shac.check().with_name().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
var checkWithNameBuiltin = newBoundBuiltin("with_name", func(ctx context.Context, s *shacState, name string, self starlark.Value, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argname starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"name?", &argname); err != nil {
		return nil, err
	}
	return self.(*check).withName(string(argname))
})

func kwargsMap(kwargs []starlark.Tuple) map[string]starlark.Value {
	res := make(map[string]starlark.Value, len(kwargs))
	for _, item := range kwargs {
		if len(item) != 2 {
			log.Panicf("kwargs item does not have length 2: %+v", kwargs)
		}
		s, ok := item[0].(starlark.String)
		if !ok {
			log.Panicf("kwargs item does not have a string key: %+v", kwargs)
		}
		res[string(s)] = item[1]
	}
	return res
}
