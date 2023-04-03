// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import (
	"context"
	"errors"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"go.fuchsia.dev/shac-project/shac/internal/reporting"
)

type checkCmd struct {
	root     string
	main     string
	allFiles bool
}

func (*checkCmd) Name() string {
	return "check"
}

func (*checkCmd) Description() string {
	return "Run checks in a file."
}

func (c *checkCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.root, "root", ".", "path to the root of the tree to analyse")
	f.StringVar(&c.main, "main", "shac.star", "main of the main shac.star")
	f.BoolVar(&c.allFiles, "all", false, "checks all the files instead of guess the upstream to diff against")
}

func (c *checkCmd) Execute(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("unsupported arguments")
	}
	return engine.Load(ctx, c.root, c.main, c.allFiles, reporting.Get())
}
