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
	"strings"

	flag "github.com/spf13/pflag"
)

var helpOut io.Writer = os.Stderr

type app struct {
	fs      *flag.FlagSet
	help    bool
	verbose bool
}

func (a *app) init(n, desc string) {
	a.fs = flag.NewFlagSet(n, flag.ContinueOnError)
	a.fs.SetOutput(helpOut)
	a.fs.BoolVarP(&a.verbose, "verbose", "v", false, "Verbose output")
	a.fs.BoolVarP(&a.help, "help", "h", false, "Prints help")
	a.fs.Usage = func() {
		fmt.Fprintf(helpOut, "Usage of %s:\n\n%s\n", n, desc)
		a.fs.PrintDefaults()
	}
}

func getDesc(s []subcommand) string {
	out := ""
	for i := range s {
		d := strings.Split(s[i].Description(), "\n")
		for i := 1; i < len(d); i++ {
			d[i] = "            " + d[i]
		}
		out += fmt.Sprintf("  %-9s %s\n", s[i].Name(), strings.Join(d, "\n"))
	}
	return out
}

type subcommand interface {
	Name() string
	Description() string
	SetFlags(*flag.FlagSet)
	Execute(ctx context.Context, args []string) error
}

// Main implements shac executable.
func Main(args []string) error {
	ctx := context.Background()

	subcommands := [...]subcommand{
		&checkCmd{},
		&docCmd{},
		&helpCmd{},
	}
	a := app{}

	if len(args) < 2 {
		a.init("shac", getDesc(subcommands[:]))
		a.fs.Usage()
		return fmt.Errorf("subcommand required")
	}
	cmd := args[1]
	if cmd == "help" {
		// Special case.
		a.init("shac", getDesc(subcommands[:]))
		a.fs.Usage()
		return flag.ErrHelp
	}
	for _, s := range subcommands {
		if s.Name() != cmd {
			continue
		}
		a.init("shac "+s.Name(), s.Description()+"\n")
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
	a.init("shac", getDesc(subcommands[:]))
	if err := a.fs.Parse(args[1:]); err != nil {
		return err
	}
	if a.help {
		a.fs.Usage()
		return flag.ErrHelp
	}
	return fmt.Errorf("no such command %q", args[1])
}
