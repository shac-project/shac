// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package shac is shac's CLI executable.
package main

import (
	"errors"
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/cli"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

func main() {
	if err := cli.Main(os.Args); err != nil && !errors.Is(err, flag.ErrHelp) {
		var stackerr engine.BacktracableError
		if errors.As(err, &stackerr) {
			_, _ = os.Stderr.WriteString(stackerr.Backtrace())
		}
		_, _ = fmt.Fprintf(os.Stderr, "shac: %s\n", err)
		os.Exit(1)
	}
}
