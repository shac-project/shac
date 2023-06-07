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

// Package shac is shac's CLI executable.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/cli"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

func main() {
	if err := cli.Main(os.Args); err != nil && !errors.Is(err, flag.ErrHelp) {
		var stackerr engine.BacktraceableError
		if errors.As(err, &stackerr) {
			_, _ = os.Stderr.WriteString(stackerr.Backtrace())
		}
		// If a check failed and stderr is a terminal, appropriate information
		// should have already been emitted by the reporter. If stderr is not a
		// terminal then it may still be useful to print the "check failed"
		// error message since the reporter output may not show up in the same
		// stream as stderr.
		if !errors.Is(err, engine.ErrCheckFailed) || !isatty.IsTerminal(os.Stderr.Fd()) {
			_, _ = fmt.Fprintf(os.Stderr, "shac: %s\n", err)
		}
		os.Exit(1)
	}
}
