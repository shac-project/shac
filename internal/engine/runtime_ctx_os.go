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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"go.fuchsia.dev/shac-project/shac/internal/execsupport"
	"go.fuchsia.dev/shac-project/shac/internal/sandbox"
	"go.starlark.net/starlark"
)

// subprocess represents an in-progress subprocess as returned by ctx.os.exec().
type subprocess struct {
	cmd            *exec.Cmd
	args           []string
	stdout         *bytes.Buffer
	stderr         *bytes.Buffer
	raiseOnFailure bool
	okRetcodes     []int
	tempDir        string

	waitCalled bool
}

var _ starlark.HasAttrs = (*subprocess)(nil)

func (s *subprocess) String() string {
	return fmt.Sprintf("<subprocess %q>", strings.Join(s.args, " "))
}

func (s *subprocess) Type() string {
	return "subprocess"
}

func (s *subprocess) Truth() starlark.Bool {
	return true
}

func (s *subprocess) Freeze() {
}

func (s *subprocess) Hash() (uint32, error) {
	return 0, errors.New("unhashable type: subprocess")
}

func (s *subprocess) Attr(name string) (starlark.Value, error) {
	switch name {
	case "wait":
		return subprocessWaitBuiltin.BindReceiver(s), nil
	default:
		return nil, nil
	}
}

func (s *subprocess) AttrNames() []string {
	return []string{"wait"}
}

func (s *subprocess) wait() (starlark.Value, error) {
	if s.waitCalled {
		return nil, fmt.Errorf("wait was already called")
	}
	s.waitCalled = true

	defer s.cleanup()

	err := s.cmd.Wait()
	retcode := 0
	if err != nil {
		var errExit *exec.ExitError
		if errors.As(err, &errExit) {
			retcode = errExit.ExitCode()
		} else {
			// Something other than a normal non-zero exit.
			return nil, err
		}
	}

	// Limits output to 10Mib. If it needs more, a file should probably be used.
	// If there is a use case, it's fine to increase.
	const limit = 10 * 1024 * 1024
	if s.stdout.Len() > limit {
		return nil, errors.New("process returned too much stdout")
	}
	if s.stderr.Len() > limit {
		return nil, errors.New("process returned too much stderr")
	}

	if !slices.Contains(s.okRetcodes, retcode) && s.raiseOnFailure {
		var msgBuilder strings.Builder
		msgBuilder.WriteString(fmt.Sprintf("command failed with exit code %d: %s", retcode, s.args))
		if s.stderr.Len() > 0 {
			msgBuilder.WriteString("\n")
			msgBuilder.WriteString(s.stderr.String())
		}
		return nil, fmt.Errorf(msgBuilder.String())
	}
	return toValue("completed_subprocess", starlark.StringDict{
		"retcode": starlark.MakeInt(retcode),
		"stdout":  starlark.String(s.stdout.String()),
		"stderr":  starlark.String(s.stderr.String()),
	}), nil
}

func (s *subprocess) cleanup() error {
	// Kill the process before doing any other cleanup steps to ensure resources
	// are no longer in use.
	err := s.cmd.Process.Kill()
	// Kill() doesn't block until the process actually completes, so we need to
	// wait before cleaning up resources.
	_ = s.cmd.Wait()

	if err2 := os.RemoveAll(s.tempDir); err == nil {
		err = err2
	}
	buffers.push(s.stdout)
	buffers.push(s.stderr)
	s.stdout, s.stderr = nil, nil

	return err
}

var subprocessWaitBuiltin = newBoundBuiltin("wait", func(ctx context.Context, s *shacState, name string, self starlark.Value, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
		return nil, err
	}
	return self.(*subprocess).wait()
})

// ctxOsExec implements the native function ctx.os.exec().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxOsExec(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argcmd starlark.Sequence
	var argcwd starlark.String
	var argenv = starlark.NewDict(0)
	var argstdin starlark.Value = starlark.None
	var argraiseOnFailure starlark.Bool = true
	var argallowNetwork starlark.Bool
	var argokRetcodes starlark.Value = starlark.None
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cmd", &argcmd,
		"cwd?", &argcwd,
		"env?", &argenv,
		"stdin?", &argstdin,
		"allow_network?", &argallowNetwork,
		"ok_retcodes?", &argokRetcodes,
		"raise_on_failure?", &argraiseOnFailure,
	); err != nil {
		return nil, err
	}
	if argcmd.Len() == 0 {
		return nil, errors.New("cmdline must not be an empty list")
	}

	var okRetcodes []int
	if argokRetcodes == starlark.None {
		okRetcodes = append(okRetcodes, 0)
	} else {
		if !argraiseOnFailure {
			return nil, fmt.Errorf("cannot combine \"ok_retcodes\" and \"raise_on_failure=False\"")
		}
		seqOkRetcodes, ok := argokRetcodes.(starlark.Sequence)
		if ok {
			okRetcodes = sequenceToInts(seqOkRetcodes)
		}
		if !ok || okRetcodes == nil {
			return nil, fmt.Errorf("for parameter \"ok_retcodes\": got %s, wanted sequence of ints", argokRetcodes)
		}
	}

	var cleanupFuncs []func() error
	defer func() {
		for _, f := range cleanupFuncs {
			// Ignore errors during cleanup because cleanupFuncs will only be
			// populated if another error occurred prior to starting the
			// subprocess.
			_ = f()
		}
	}()

	tempDir, err := s.newTempDir()
	if err != nil {
		return nil, err
	}

	cleanupFuncs = append(cleanupFuncs, func() error {
		return os.RemoveAll(tempDir)
	})

	stdout := buffers.get()
	stderr := buffers.get()

	cleanupFuncs = append(cleanupFuncs, func() error {
		buffers.push(stdout)
		buffers.push(stderr)
		return nil
	})

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

	var passthroughMounts []sandbox.Mount
	for _, pte := range s.passthroughEnv {
		val, ok := os.LookupEnv(pte.Name)
		if !ok {
			continue
		}
		env[pte.Name] = val
		if pte.IsPath {
			passthroughMounts = append(passthroughMounts, sandbox.Mount{
				Path:     val,
				Writable: pte.Writeable,
			})
		}
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

	var stdin io.Reader
	switch s := argstdin.(type) {
	case starlark.String:
		stdin = strings.NewReader(string(s))
	case starlark.Bytes:
		stdin = bytes.NewReader([]byte(s))
	case starlark.NoneType:
	default:
		return nil, fmt.Errorf("for parameter \"stdin\": got %s, want str or bytes", argstdin.Type())
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

	if filepath.IsAbs(fullCmd[0]) {
		// Stat to make sure the entrypoint executable exists rather than
		// letting nsjail fail, for consistency with the non-absolute path case.
		if _, err = os.Stat(fullCmd[0]); err != nil {
			return nil, err
		}
	} else {
		// nsjail doesn't do $PATH-based resolution of the command it's given.
		// Do this resolution unconditionally for consistency across platforms
		// even though it's not necessary when not using nsjail. This also
		// ensures that relative file paths are interpreted relative to the root
		// directory, rather than the directory from which shac is run.
		absPath := filepath.Join(s.root, s.subdir, fullCmd[0])
		if _, err = os.Stat(absPath); err != nil {
			// exec.LookPath() doesn't do $PATH-based lookup for paths
			// containing slashes, so no point in trying it if the file doesn't
			// exist.
			if !errors.Is(err, os.ErrNotExist) || strings.Contains(fullCmd[0], "/") {
				return nil, err
			}
			// If the path doesn't exist in the root, fall back to a $PATH
			// lookup.
			absPath, err = exec.LookPath(fullCmd[0])
			if err != nil {
				return nil, err
			}
		}
		fullCmd[0] = absPath
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
			// TODO(olivernewman): Mount the checkout read-only unconditionally.
			{Path: s.root, Writable: s.writableRoot},
			// OS-provided utilities.
			{Path: "/dev/null", Writable: true},
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
			{Path: filepath.Dir(tempDir), Writable: true},
		}
		config.Mounts = append(config.Mounts, passthroughMounts...)

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
			if !filepath.IsAbs(p) {
				// Relative paths in $PATH are not allowed.
				continue
			}
			var fi os.FileInfo
			if fi, err = os.Stat(p); err != nil || !fi.IsDir() {
				// Skip $PATH elements that don't exist or point to
				// non-directories.
				continue
			}
			config.Mounts = append(config.Mounts, sandbox.Mount{Path: p})
		}
	}

	cmd := s.sandbox.Command(ctx, config)

	cmd.Stdin = stdin
	// TODO(olivernewman): Also handle commands that may output non-utf-8 bytes.
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = execsupport.Start(cmd)
	if err != nil {
		return nil, err
	}

	proc := &subprocess{
		cmd:            cmd,
		args:           sequenceToStrings(argcmd),
		stdout:         stdout,
		stderr:         stderr,
		raiseOnFailure: bool(argraiseOnFailure),
		okRetcodes:     okRetcodes,
		tempDir:        tempDir,
	}
	// Only clean up now if starting the subprocess failed; otherwise it will
	// get cleaned up by wait().
	cleanupFuncs = cleanupFuncs[:0]

	chk := ctxCheck(ctx)
	chk.subprocesses = append(chk.subprocesses, proc)
	return proc, nil
}

// sequenceToInts converts a starlark sequence (list, tuple) into a slice of
// ints.
func sequenceToInts(s starlark.Sequence) []int {
	out := make([]int, 0, s.Len())
	iter := s.Iterate()
	var x starlark.Value
	for iter.Next(&x) {
		i, ok := x.(starlark.Int)
		if !ok {
			return nil
		}
		i64, ok := i.Int64()
		if !ok {
			return nil
		}
		if i64 > math.MaxInt || i64 < math.MinInt {
			return nil
		}
		out = append(out, int(i64))
	}
	return out
}
