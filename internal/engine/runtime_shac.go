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
	"runtime/debug"

	"go.starlark.net/starlark"
)

var (
	// Version is the current tool version.
	//
	// TODO(maruel): Add proper version, preferably from git tag.
	Version = [...]int{0, 1, 2}
)

// getShac returns the global shac object.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func getShac() starlark.StringDict {
	return starlark.StringDict{
		"check":          newBuiltin("shac.check", shacCheck),
		"commit_hash":    starlark.String(getCommitHash()),
		"register_check": newBuiltinNone("shac.register_check", shacRegisterCheck),
		"version": starlark.Tuple{
			starlark.MakeInt(Version[0]), starlark.MakeInt(Version[1]), starlark.MakeInt(Version[2]),
		},
	}
}

// shacRegisterCheck implements native function shac.register_check().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func shacRegisterCheck(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var argcheck starlark.Value
	if err := starlark.UnpackArgs(name, args, kwargs,
		"check", &argcheck); err != nil {
		return err
	}
	// Inspect callback to verify that it accepts one argument and that it is not a builtin.
	var c *check
	switch x := argcheck.(type) {
	case starlark.Callable:
		var err error
		c, err = newCheck(x, "", false)
		if err != nil {
			return err
		}
	case *check:
		c = x
	default:
		return fmt.Errorf("\"check\" must be a function or shac.check object, got %s", x.Type())
	}
	// We may want to optimize this if we register hundreds of checks.
	for i := range s.checks {
		if s.checks[i].name == c.name {
			return fmt.Errorf("can't register two checks with the same name %q", c.name)
		}
	}
	if s.doneLoading {
		return errors.New("can't register checks after done loading")
	}
	// Register the new callback.
	s.checks = append(s.checks, registeredCheck{check: c})
	return nil
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
