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

// Package nsjail contains a local copy of nsjail for linux.
package nsjail

// TODO(olivernewman): Generalize this package to provide sandboxing that works
// for macOS and Windows as well, choosing the appropriate OS-specific
// sandboxing method under the hood. Also ideally stop depending on an nsjail
// prebuilt for Linux and instead call the appropriate Linux APIs directly.

import (
	"fmt"
	"os"
	"path/filepath"
)

//go:generate go run download.go

// WriteExecutable writes the embedded nsjail binary to an executable file.
func WriteExecutable(dir string) (string, error) {
	// Don't write the binary if we're not on a supported platform.
	if len(Exec) == 0 {
		return "", nil
	}
	nsjailPath := filepath.Join(dir, "nsjail")
	// Executable permissions are ok.
	//#nosec CWE-276
	if err := os.WriteFile(nsjailPath, Exec, 0o700); err != nil {
		return "", err
	}
	return nsjailPath, nil
}

// Mount represents a directory or file from the filesystem to mount inside the
// nsjail so that processes inside the nsjail can access it.
type Mount struct {
	// Path outside the nsjail that should be mounted inside the nsjail.
	Path string
	// Dest is the optional location to mount in the nsjail. If omitted, it will
	// be assumed to be the same as Path.
	Dest string
	// Writeable controls whether the mount is writeable by processes within the
	// nsjail.
	Writeable bool
}

// Config represents a the configuration for an nsjail invocation. It can be
// translated to command line flags.
type Config struct {
	// Path to nsjail.
	Nsjail       string
	Cwd          string
	Env          map[string]string
	Mounts       []Mount
	AllowNetwork bool

	// Require keyed arguments.
	_ struct{}
}

func (c *Config) Wrap(cmd []string) []string {
	res := []string{
		c.Nsjail,
		"--quiet",
		"--forward_signals",
		// Limits on file read sizes are not useful.
		"--disable_rlimits",
		"--disable_clone_newcgroup",
		// Time limits are not useful.
		"--time_limit", "0",
		"--cwd", c.Cwd,
	}
	if c.AllowNetwork {
		res = append(res, "--disable_clone_newnet")
	}
	for k, v := range c.Env {
		res = append(res, "--env", fmt.Sprintf("%s=%s", k, v))
	}
	for _, mnt := range c.Mounts {
		flag := "--bindmount_ro"
		if mnt.Writeable {
			flag = "--bindmount"
		}
		val := mnt.Path
		if mnt.Dest != "" {
			val = fmt.Sprintf("%s:%s", mnt.Path, mnt.Dest)
		}
		res = append(res, flag, val)
	}
	res = append(res, "--")
	return append(res, cmd...)
}
