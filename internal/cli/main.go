// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	flag "github.com/spf13/pflag"
)

type app struct {
	fs      *flag.FlagSet
	help    bool
	verbose bool
}

func (a *app) init(n string) {
	a.fs = flag.NewFlagSet(n, flag.ContinueOnError)
	a.fs.BoolVarP(&a.verbose, "verbose", "v", false, "Verbose output")
	a.fs.BoolVarP(&a.help, "help", "h", false, "Prints help")
	a.fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", n)
		a.fs.PrintDefaults()
	}
}

type subcommand interface {
	Name() string
	Description() string
	SetFlags(*flag.FlagSet)
	Execute(ctx context.Context, args []string) error
}

func Main(args []string) error {
	ctx := context.Background()

	if len(args) < 2 {
		a := app{}
		a.init("shac")
		a.fs.Usage()
		return fmt.Errorf("subcommand required")
	}

	subcommands := []subcommand{
		&docCmd{},
		&checkCmd{},
	}

	name := args[1]
	for _, s := range subcommands {
		if s.Name() != name {
			continue
		}
		a := app{}
		a.init("shac " + s.Name())
		s.SetFlags(a.fs)
		if err := a.fs.Parse(args[2:]); err != nil {
			return err
		}
		if a.help {
			a.fs.Usage()
			return flag.ErrHelp
		}
		if !a.verbose {
			log.SetOutput(io.Discard)
		}
		return s.Execute(ctx, a.fs.Args())
	}
	a := app{}
	a.init("shac")
	if err := a.fs.Parse(args[1:]); err != nil {
		return err
	}
	if a.help {
		a.fs.Usage()
		return flag.ErrHelp
	}
	return fmt.Errorf("no such command %q", args[1])
}
