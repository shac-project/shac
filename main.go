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
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/mattn/go-isatty"
	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/cli"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

func main() {
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, syscall.SIGTERM, syscall.SIGINT)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := <-signalChannel
		cancel()
		// Print a goroutine stacktrace only on SIGTERM - we only want to see a
		// stack trace when shac gets canceled by automation, which may indicate
		// a timeout due to a hang. If shac gets Ctrl-C'd (SIGINT) by a human
		// user it's not helpful to print a stacktrace.
		if sig == syscall.SIGTERM {
			_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
		}
	}()

	if err := cli.Main(ctx, os.Args); err != nil && !errors.Is(err, flag.ErrHelp) {
		var stackerr engine.BacktraceableError
		if errors.As(err, &stackerr) {
			_, _ = os.Stderr.WriteString(stackerr.Backtrace())
		}
		// If stderr is not a terminal, always print the error.
		//
		// If stderr is a terminal:
		// - If a check failed, appropriate information should have already been
		//   emitted by the reporter.
		// - A context cancellation will likely be because the user Ctrl-C'd
		//   shac, so the exit will be expected and there's no need to print
		//   anything.
		if !isatty.IsTerminal(os.Stderr.Fd()) ||
			(!errors.Is(err, engine.ErrCheckFailed) && !errors.Is(err, context.Canceled)) {
			_, _ = fmt.Fprintf(os.Stderr, "shac: %s\n", err)
		}
		os.Exit(1)
	}
}
