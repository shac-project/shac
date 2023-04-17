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
	"path/filepath"
	"runtime"
	"strings"

	"go.fuchsia.dev/shac-project/shac/internal/nsjail"
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
	var argallowNetwork starlark.Bool
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cmd", &argcmd,
		"cwd?", &argcwd,
		"env?", &argenv,
		"raise_on_failure?", &argraiseOnFailure,
		"allow_network?", &argallowNetwork,
	); err != nil {
		return nil, err
	}
	if argcmd.Len() == 0 {
		return nil, errors.New("cmdline must not be an empty list")
	}

	parsedEnv := map[string]string{}
	for _, item := range argenv.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("\"env\" key is not a string: %s", item[0])
		}
		v, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("\"env\" value is not a string: %s", item[1])
		}
		parsedEnv[string(k)] = string(v)
	}

	cwd := s.root
	if string(argcwd) != "" {
		var err error
		cwd, err = absPath(string(argcwd), s.root)
		if err != nil {
			return nil, err
		}
	}

	parsedCmd := sequenceToStrings(argcmd)
	if parsedCmd == nil {
		return nil, fmt.Errorf("for parameter \"cmd\": got %s, want sequence of str", argcmd.Type())
	}

	tempDir, err := s.newTempDir()
	if err != nil {
		return nil, err
	}
	// TODO(olivernewman): Catch errors.
	defer os.RemoveAll(tempDir)

	var fullCmd []string
	if nsjail.Supported() {
		// TODO(olivernewman): Cache the written nsjail executable.
		var nsjailDir string
		if nsjailDir, err = os.MkdirTemp("", "nsjail"); err != nil {
			return nil, err
		}
		// TODO(olivernewman): Catch errors.
		defer os.RemoveAll(nsjailDir)
		nsjailPath := filepath.Join(nsjailDir, "nsjail")
		// Executable permissions are ok.
		//#nosec CWE-276
		if err = os.WriteFile(nsjailPath, nsjail.Exec, 0o700); err != nil {
			return nil, err
		}

		fullCmd = append(fullCmd,
			nsjailPath,
			"--quiet",
			"--forward_signals",
			// Limits on file read sizes are not useful.
			"--disable_rlimits",
			"--disable_clone_newcgroup",
			// Time limits are not useful.
			"--time_limit", "0",
			"--cwd", cwd,
			// TODO(olivernewman): Use a hermetic Go installation, don't add
			// $GOROOT to $PATH.
			"--env", "PATH=/usr/bin:/bin:"+filepath.Join(runtime.GOROOT(), "bin"),
			"--bindmount", fmt.Sprintf("%s:/tmp", tempDir),
			// TODO(olivernewman): Mount the checkout read-only by default.
			"--bindmount", s.root,
		)
		for k, v := range parsedEnv {
			fullCmd = append(fullCmd, "--env", fmt.Sprintf("%s=%s", k, v))
		}
		fullCmd = append(fullCmd, "--env", "TEMP=/tmp")
		fullCmd = append(fullCmd, "--env", "TMPDIR=/tmp")
		fullCmd = append(fullCmd, "--env", "TEMPDIR=/tmp")

		// Mount $GOROOT unless it's a subdirectory of the checkout dir, in
		// which case it will already be mounted.
		// TODO(olivernewman): Use a hermetic go installation for shac's own
		// checks, so we don't need to special-case $GOROOT.
		if !strings.HasPrefix(runtime.GOROOT(), s.root+string(os.PathSeparator)) {
			fullCmd = append(fullCmd, "--bindmount_ro", runtime.GOROOT())
		}
		for _, mount := range allowedMounts {
			fullCmd = append(fullCmd, "--bindmount_ro", mount)
		}
		if argallowNetwork {
			fullCmd = append(fullCmd, "--disable_clone_newnet")
		}

		fullCmd = append(fullCmd, "--")

		parsedCmd[0], err = exec.LookPath(parsedCmd[0])
		if err != nil && !errors.Is(err, exec.ErrDot) {
			return nil, err
		}
	}
	fullCmd = append(fullCmd, parsedCmd...)

	//#nosec G204
	cmd := exec.CommandContext(ctx, fullCmd[0], fullCmd[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// TODO(olivernewman): Also handle commands that may output non-utf-8 bytes.
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if !nsjail.Supported() {
		cmd.Dir = cwd
		cmd.Env = os.Environ()
		for k, v := range parsedEnv {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = append(cmd.Env, "TEMP="+tempDir)
		cmd.Env = append(cmd.Env, "TMPDIR="+tempDir)
		cmd.Env = append(cmd.Env, "TEMPDIR="+tempDir)
	}

	var retcode int
	if err = cmd.Run(); err != nil {
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

var allowedMounts = [...]string{
	// System binaries.
	"/bin",
	// OS-provided utilities.
	"/dev/null",
	"/dev/urandom",
	"/dev/zero",
	// DNS configs.
	"/etc/nsswitch.conf",
	"/etc/resolv.conf",
	// Required for https.
	"/etc/ssl/certs",
	// These are required for bash to work.
	"/lib",
	"/lib64",
	// More system binaries.
	"/usr/bin",
	// OS header files.
	"/usr/include",
	// System compilers.
	"/usr/lib",
}
