// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import (
	"context"
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
)

type app struct {
	topLevelFlags *flag.FlagSet
	subcommands   []*flag.FlagSet
}

type subcommand interface {
	Name() string
	Description() string
	SetFlags(*flag.FlagSet)
	Execute(context.Context, *flag.FlagSet) error
}

func Main() error {
	ctx := context.Background()

	if len(os.Args) < 2 {
		return fmt.Errorf("subcommand required")
	}

	subcommands := []subcommand{
		&checkCmd{},
	}

	name := os.Args[1]
	for _, s := range subcommands {
		if s.Name() != name {
			continue
		}
		fs := flag.NewFlagSet(s.Name(), flag.ContinueOnError)
		s.SetFlags(fs)
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		return s.Execute(ctx, fs)
	}
	return fmt.Errorf("no such command %q", name)
}
