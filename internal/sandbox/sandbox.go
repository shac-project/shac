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

// Package sandbox provides capabilities for sandboxing subprocesses.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"go.fuchsia.dev/shac-project/shac/internal/execsupport"
)

//go:generate go run download_nsjail.go

// Mount represents a directory or file from the filesystem to mount inside the
// nsjail so that processes inside the nsjail can access it.
type Mount struct {
	// Path outside the nsjail that should be mounted inside the nsjail.
	Path string
	// Dest is the optional location to mount in the nsjail. If omitted, it will
	// be assumed to be the same as Path.
	Dest string
	// Writable controls whether the mount is writable by processes within the
	// nsjail.
	Writable bool
}

// Config represents the configuration for a sandboxed subprocess.
type Config struct {
	// The subprocess command line.
	Cmd          []string
	Cwd          string
	AllowNetwork bool
	Env          map[string]string
	Mounts       []Mount

	// Require keyed arguments.
	_ struct{}
}

type Sandbox interface {
	Command(context.Context, *Config) *exec.Cmd
}

// New constructs a platform-appropriate sandbox.
func New(tempDir string) (Sandbox, error) {
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		execsupport.Mu.Lock()
		defer execsupport.Mu.Unlock()
		nsjailPath := filepath.Join(tempDir, "nsjail")
		// O_CLOEXEC prevents subprocesses from inheriting the open FD. This
		// doesn't happen in production, but shac tests run many shac instances
		// in parallel in the same subprocess and it causes issues if those
		// subprocesses do forks that inherit the open FD.
		flag := os.O_WRONLY | os.O_CREATE | syscall.O_CLOEXEC
		// Executable permissions are ok.
		//#nosec CWE-276
		f, err := os.OpenFile(nsjailPath, flag, 0o700)
		if err != nil {
			return nil, err
		}
		_, err = f.Write(nsjailExecutableBytes)
		if err2 := f.Close(); err == nil {
			err = err2
		}
		if err != nil {
			return nil, err
		}
		return nsjailSandbox{nsjailPath: nsjailPath}, nil
	} else if runtime.GOOS == "darwin" {
		return macSandbox{}, nil
	}
	// TODO(olivernewman): Provide stricter sandboxing for Windows.
	return genericSandbox{}, nil
}

// nsjailSandbox provides sandboxing for Linux using nsjail.
//
// TODO(olivernewman): Replace this with a solution that uses cgroups instead of
// depending on a prebuilt nsjail executable.
type nsjailSandbox struct {
	nsjailPath string
}

func (s nsjailSandbox) Command(ctx context.Context, config *Config) *exec.Cmd {
	args := []string{
		"--quiet",
		"--forward_signals",
		// Limits on file read sizes are not useful.
		"--disable_rlimits",
		"--disable_clone_newcgroup",
		// Time limits are not useful.
		"--time_limit", "0",
		"--cwd", config.Cwd,
	}
	if config.AllowNetwork {
		args = append(args, "--disable_clone_newnet")
	}
	env := config.Env
	for k, v := range env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}
	// nsjail is strict about ordering of --bindmount flags. If /a and /a/b are
	// both to be mounted (/a might be read-only while /a/b is writable), then
	// /a must precede /a/b in the arguments.
	sort.Slice(config.Mounts, func(i, j int) bool {
		return config.Mounts[i].Path < config.Mounts[j].Path
	})
	for _, mnt := range config.Mounts {
		flag := "--bindmount_ro"
		if mnt.Writable {
			flag = "--bindmount"
		}
		val := mnt.Path
		if mnt.Dest != "" {
			val = fmt.Sprintf("%s:%s", mnt.Path, mnt.Dest)
		}
		args = append(args, flag, val)
	}
	args = append(args, "--")
	args = append(args, config.Cmd...)
	return exec.CommandContext(ctx, s.nsjailPath, args...)
}

// macSandbox provides a sandbox specific to macOS using the preinstalled
// sandbox-exec tool.
//
// It only supports network access restrictions.
type macSandbox struct{}

func (s macSandbox) Command(ctx context.Context, config *Config) *exec.Cmd {
	profile := []string{
		"(version 1)",
		"(allow default)",
	}

	if !config.AllowNetwork {
		profile = append(profile, "(deny network*)")
	}

	args := append([]string{"-p", strings.Join(profile, "\n")}, config.Cmd...)
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Dir = config.Cwd

	for k, v := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// config.Mounts intentionally ignored.
	// TODO(olivernewman): Also restrict filesystem access, note that it may not
	// be possible to mount a file at a different path.

	return cmd
}

// genericSandbox provides a limited sandbox that works on any OS.
//
// Filesystem and network access restrictions are not supported.
type genericSandbox struct{}

func (s genericSandbox) Command(ctx context.Context, config *Config) *exec.Cmd {
	cmd := exec.CommandContext(ctx, config.Cmd[0], config.Cmd[1:]...)
	cmd.Dir = config.Cwd

	for k, v := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// config.Mounts and config.AllowNetwork intentionally ignored.

	return cmd
}
