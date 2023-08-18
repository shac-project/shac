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
	"sync"
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

// Mu works around fork+exec concurrency issue with open file handle on POSIX.
// See https://github.com/golang/go/issues/22315 and
// https://github.com/golang/go/issues/22220 for background.
//
// This is only needed when running unit tests.
var Mu sync.RWMutex

// New constructs a platform-appropriate sandbox.
func New(tempDir string) (Sandbox, error) {
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		Mu.Lock()
		defer Mu.Unlock()
		nsjailPath := filepath.Join(tempDir, "nsjail")
		// Executable permissions are ok.
		//#nosec CWE-276
		if err := os.WriteFile(nsjailPath, nsjailExecutableBytes, 0o700); err != nil {
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
	//#nosec G204
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
