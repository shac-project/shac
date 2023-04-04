// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reporting

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

// Get returns the right reporting implementation based on the current
// environment.
func Get() engine.Report {
	// On LUCI/Swarming. ResultDB!
	if os.Getenv("SWARMING_TASK_ID") != "" {
		// TODO(maruel): Emits LUCI_CONTEXT.
		return &basic{}
	}

	// On GitHub Actions.
	if os.Getenv("GITHUB_RUN_ID") != "" {
		// Emits GitHub Workflows commands.
		return &github{}
	}

	// Active terminal. Colors! This includes VSCode's integrated terminal.
	if os.Getenv("TERM") != "dumb" && isatty.IsTerminal(os.Stderr.Fd()) {
		return &interactive{
			out: colorable.NewColorableStdout(),
		}
	}

	// VSCode extension.
	if os.Getenv("VSCODE_GIT_IPC_HANDLE") != "" {
		// TODO(maruel): Return SARIF.
		return &basic{}
	}

	// Anything else, e.g. redirected output.
	return &basic{}
}

type basic struct {
}

func (basic) Print(ctx context.Context, file string, line int, message string) {
	fmt.Fprintf(os.Stderr, "[%s:%d] %s\n", file, line, message)
}

// github is the Report implementation when running inside a GitHub Actions
// Workflow.
//
// See https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions
type github struct {
}

func (github) Print(ctx context.Context, file string, line int, message string) {
	fmt.Fprintf(os.Stdout, "::notice file=%s,line=%d::%s\n", file, line, message)
}

type interactive struct {
	out io.Writer
}

func (interactive) Print(ctx context.Context, file string, line int, message string) {
	// https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_.28Select_Graphic_Rendition.29_parameters
	fmt.Fprintf(os.Stderr, "\x1b[0m[\x1b[32m%s:%d\x1b[0m] \x1b[1m%s\x1b[0m\n", file, line, message)
}
