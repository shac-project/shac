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
	for _, c := range s {
		d := strings.Split(c.Description(), "\n")
		for i := 1; i < len(d); i++ {
			d[i] = "            " + d[i]
		}
		out += fmt.Sprintf("  %-9s %s\n", c.Name(), strings.Join(d, "\n"))
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
func Main(ctx context.Context, args []string) error {
	subcommands := [...]subcommand{
		// Ordered roughly by importance, because ordering here corresponds to
		// the order in which subcommands will be listed in `shac help`.
		&checkCmd{},
		&fmtCmd{},
		&fixCmd{},
		&docCmd{},
		&versionCmd{},
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
