// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cli is the shac CLI code.
package cli

import (
	"context"
	"errors"
	"os"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

type docCmd struct {
}

func (*docCmd) Name() string {
	return "doc"
}

func (*docCmd) Description() string {
	return "Prints out documentation for a starlark file. Use \"stdlib\" to print out the standard library documentation."
}

func (*docCmd) SetFlags(f *flag.FlagSet) {
}

func (d *docCmd) Execute(ctx context.Context, args []string) error {
	f := "stdlib"
	if len(args) == 1 {
		f = args[0]
	} else if len(args) > 1 {
		return errors.New("only specify one source")
	}
	doc, err := engine.Doc(f)
	if err != nil {
		return err
	}
	os.Stdout.WriteString(doc)
	return nil
}
