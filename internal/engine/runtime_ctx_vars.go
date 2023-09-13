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

	"go.starlark.net/starlark"
)

// ctxVarsGet implements native function ctx.vars.get().
//
// It returns a string, or an error if the requested variable is not a valid
// variable listed in the project's shac.textproto config file.
//
// The full dictionary of available variables is intentionally not exposed to
// user code because that would allow probing the variables from shared check
// libraries, which would make variable names part of the API of those
// libraries. Variables should only be used *within* a single project.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxVarsGet(ctx context.Context, s *shacState, funcname string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argname starlark.String
	if err := starlark.UnpackArgs(funcname, args, kwargs,
		"name", &argname,
	); err != nil {
		return nil, err
	}
	name := string(argname)
	if name == "" {
		return nil, errors.New("for parameter \"name\": must not be empty")
	}
	val, ok := s.vars[name]
	if !ok {
		return nil, fmt.Errorf("unknown variable %q", name)
	}
	return starlark.String(val), nil
}
