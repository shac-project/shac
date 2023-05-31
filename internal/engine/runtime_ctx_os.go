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

	"go.fuchsia.dev/shac-project/shac/internal/sandbox"
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

	tempDir, err := s.newTempDir()
	if err != nil {
		return nil, err
	}
	// TODO(olivernewman): Catch errors.
	defer os.RemoveAll(tempDir)

	env := map[string]string{
		"PATH":    os.Getenv("PATH"),
		"TEMP":    tempDir,
		"TMPDIR":  tempDir,
		"TEMPDIR": tempDir,
	}
	if runtime.GOROOT() != "" {
		// TODO(olivernewman): This is necessary because checks for shac itself
		// assume Go is pre-installed. Switch to a hermetic Go installation that
		// installs Go in the checkout directory, and stop explicitly mounting
		// $GOROOT and adding it to $PATH.
		env["PATH"] = strings.Join([]string{
			filepath.Join(runtime.GOROOT(), "bin"),
			env["PATH"],
		}, string(os.PathListSeparator))
	}
	for _, item := range argenv.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("\"env\" key is not a string: %s", item[0])
		}
		// TODO(olivernewman): This is unnecessarily strict - commands should
		// not set $PATH in `env`, but we should allow prepending to $PATH with
		// `env_prefixes`, and add an option to not inherit the value of $PATH
		// if better hermeticity is desired.
		if k == "PATH" {
			return nil, fmt.Errorf("$PATH cannot be overridden")
		}
		v, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("\"env\" value is not a string: %s", item[1])
		}
		env[string(k)] = string(v)
	}

	cwd := filepath.Join(s.root, s.subdir)
	if s := string(argcwd); s != "" {
		cwd, err = absPath(s, cwd)
		if err != nil {
			return nil, err
		}
	}

	fullCmd := sequenceToStrings(argcmd)
	if fullCmd == nil {
		return nil, fmt.Errorf("for parameter \"cmd\": got %s, want sequence of str", argcmd.Type())
	}

	exeParts := strings.Split(fullCmd[0], string(os.PathSeparator))
	if exeParts[0] == "." {
		// exec.Command doesn't evaluate ".", so convert to an absolute path.
		exeParts[0] = s.root
		fullCmd[0] = strings.Join(exeParts, string(os.PathSeparator))
	} else {
		// nsjail doesn't do $PATH-based resolution of the command it's given, so it
		// must either be an absolute or relative path. Do this resolution
		// unconditionally for consistency across platforms even though it's not
		// necessary when not using nsjail.
		fullCmd[0], err = exec.LookPath(fullCmd[0])
		if err != nil && !errors.Is(err, exec.ErrDot) {
			return nil, err
		}
	}

	config := &sandbox.Config{
		Cmd:          fullCmd,
		Cwd:          cwd,
		AllowNetwork: bool(argallowNetwork),
		Env:          env,
	}
	// config.Mounts is ignored for the moment on Windows.
	if runtime.GOOS != "windows" {
		config.Mounts = []sandbox.Mount{
			// TODO(olivernewman): Mount the checkout read-only by default.
			{Path: s.root, Writeable: true},
			// OS-provided utilities.
			{Path: "/dev/null", Writeable: true},
			{Path: "/dev/urandom"},
			{Path: "/dev/zero"},
			// DNS configs.
			{Path: "/etc/nsswitch.conf"},
			{Path: "/etc/resolv.conf"},
			// Required for https.
			{Path: "/etc/ssl/certs"},
			// These are required for bash to work.
			{Path: "/lib"},
			{Path: "/lib64"},
			// OS header files.
			{Path: "/usr/include"},
			// System compilers.
			{Path: "/usr/lib"},
			// Make the parent directory of tempDir available, since it is the root
			// of all ctx.os.tempdir() calls, which can be used as scratch pads for
			// this executable.
			{Path: filepath.Dir(tempDir), Writeable: true},
		}

		// TODO(olivernewman): This is necessary because checks for shac itself
		// assume Go is pre-installed. Switch to a hermetic Go installation that
		// installs Go in the checkout directory, and stop explicitly mounting
		// $GOROOT and adding it to $PATH.
		if runtime.GOROOT() != "" {
			config.Mounts = append(config.Mounts, sandbox.Mount{Path: runtime.GOROOT()})
		}

		// Mount all directories listed in $PATH.
		for _, p := range strings.Split(env["PATH"], string(os.PathListSeparator)) {
			// $PATH may contain invalid elements. Filter them out.
			var fi os.FileInfo
			if fi, err = os.Stat(p); err != nil || !fi.IsDir() {
				continue
			}
			config.Mounts = append(config.Mounts, sandbox.Mount{Path: p})
		}
	}

	cmd := s.sandbox.Command(ctx, config)

	stdout := buffers.get()
	stderr := buffers.get()
	defer func() {
		buffers.push(stdout)
		buffers.push(stderr)
	}()
	// TODO(olivernewman): Also handle commands that may output non-utf-8 bytes.
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	retcode, err := execCmd(cmd)
	// Limits output to 10Mib. If it needs more, a file should probably be used.
	// If there is a use case, it's fine to increase.
	const limit = 10 * 1024 * 1024
	if stdout.Len() > limit {
		return nil, errors.New("process returned too much stdout")
	}
	if stderr.Len() > limit {
		return nil, errors.New("process returned too much stderr")
	}
	if err != nil {
		return nil, err
	}
	if retcode != 0 && argraiseOnFailure {
		var msgBuilder strings.Builder
		msgBuilder.WriteString(fmt.Sprintf("command failed with exit code %d: %s", retcode, argcmd))
		if stderr.Len() > 0 {
			msgBuilder.WriteString("\n")
			msgBuilder.WriteString(stderr.String())
		}
		return nil, fmt.Errorf(msgBuilder.String())
	}
	return toValue("completed_subprocess", starlark.StringDict{
		"retcode": starlark.MakeInt(retcode),
		"stdout":  starlark.String(stdout.String()),
		"stderr":  starlark.String(stderr.String()),
	}), nil
}

func execCmd(cmd *exec.Cmd) (int, error) {
	// Serialize start given the issue described at sandbox.Mu.
	sandbox.Mu.RLock()
	err := cmd.Start()
	sandbox.Mu.RUnlock()
	if err != nil {
		// The executable didn't start.
		return 0, err
	}
	if err = cmd.Wait(); err == nil {
		// Happy path.
		return 0, nil
	}
	var errExit *exec.ExitError
	if !errors.As(err, &errExit) {
		// Something else than an normal non-zero exit.
		return 0, err
	}
	return errExit.ExitCode(), nil
}
