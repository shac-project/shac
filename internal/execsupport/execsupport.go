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

// Package execsupport implements wrappers around os/exec.Cmd Start() and Wait()
// functions that acquire a read lock on a R/W mutex to work around fork+exec
// concurrency issue with open file handle on POSIX.
// See https://github.com/golang/go/issues/22315 and
// https://github.com/golang/go/issues/22220 for background.
//
// This specifically affects the nsjail executable: if one goroutine of a
// subprocess has the nsjail file handle open for writing at the time that
// another goroutine forks a subprocess (any subprocess, whether it be git or a
// subprocess run by `ctx.os.exec()`), the forked subprocess will inherit the
// open file handle and it will remain open even after the parent closes it.
// Then if the parent or any other process tries to run the nsjail executable,
// it will fail with ETXTBSY due to being open for writing.
//
// So the code that writes the nsjail executable must acquire this package's
// mutex for writing, to prevent any forks while the nsjail file is open for
// writing, and all shac code must use this package's Start() and Wait()
// functions instead of calling Cmd.Start() and Cmd.Wait() directly.
//
// This is only strictly necessary when running unit tests, because many shac
// instances are run in parallel in the same process. In normal execution,
// there are no subprocesses forks run concurrently when the nsjail executable
// is being written.
//
// TODO(olivernewman): This hack will no longer be necessary when we switch from
// using a prebuilt nsjail to implementing our own sandboxing using cgroups.
package execsupport

import (
	"os/exec"
	"sync"
)

// Mu enables blocking all exec(), for example while writing an executable that
// will later be exec()'ed.
var Mu sync.RWMutex

// Start is a fork-safe wrapper around os/exec.Cmd.Start.
func Start(cmd *exec.Cmd) error {
	Mu.RLock()
	defer Mu.RUnlock()
	return cmd.Start()
}

// Run is a fork-safe wrapper around os/exec.Cmd.Run.
func Run(cmd *exec.Cmd) error {
	Mu.RLock()
	defer Mu.RUnlock()
	return cmd.Run()
}
