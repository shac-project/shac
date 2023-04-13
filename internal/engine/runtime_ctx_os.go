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
	"os"
	"os/exec"
	"strings"

	"go.starlark.net/starlark"
)

// ctxOsExec implements the native function ctx.os.exec().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxOsExec(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argcmd starlark.Sequence
	var argcwd starlark.String
	var argenv = starlark.NewDict(0)
	var argraiseOnFailure starlark.Bool = true
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cmd", &argcmd,
		"cwd?", &argcwd,
		"env?", &argenv,
		"raise_on_failure?", &argraiseOnFailure,
	); err != nil {
		return nil, err
	}
	if argcmd.Len() == 0 {
		return nil, errors.New("cmdline must not be an empty list")
	}

	parsedCmd := sequenceToStrings(argcmd)
	if parsedCmd == nil {
		return nil, fmt.Errorf("for parameter \"cmd\": got %s, want sequence of str", argcmd.Type())
	}

	// TODO(olivernewman): Wrap with nsjail on linux.
	//#nosec G204
	cmd := exec.CommandContext(ctx, parsedCmd[0], parsedCmd[1:]...)

	if argcwd.GoString() != "" {
		var err error
		cmd.Dir, err = absPath(argcwd.GoString(), s.root)
		if err != nil {
			return nil, err
		}
	} else {
		cmd.Dir = s.root
	}

	// TODO(olivernewman): Also handle commands that may output non-utf-8 bytes.
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Env = os.Environ()
	for _, item := range argenv.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("\"env\" key is not a string: %s", item[0])
		}
		v, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("\"env\" value is not a string: %s", item[1])
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", string(k), string(v)))
	}

	err := cmd.Run()
	var retcode int
	if err != nil {
		var errExit *exec.ExitError
		if errors.As(err, &errExit) {
			if argraiseOnFailure {
				var msgBuilder strings.Builder
				msgBuilder.WriteString(fmt.Sprintf("command failed with exit code %d: %s", errExit.ExitCode(), argcmd))
				if stderr.Len() > 0 {
					msgBuilder.WriteString("\n")
					msgBuilder.WriteString(stderr.String())
				}
				return nil, fmt.Errorf(msgBuilder.String())
			}
			retcode = errExit.ExitCode()
		} else {
			return nil, err
		}
	}

	return toValue("completed_subprocess", starlark.StringDict{
		"retcode": starlark.MakeInt(retcode),
		"stdout":  starlark.String(stdout.String()),
		"stderr":  starlark.String(stderr.String()),
	}), nil
}
