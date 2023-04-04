// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import (
	"context"

	flag "github.com/spf13/pflag"
)

type helpCmd struct {
}

func (*helpCmd) Name() string {
	return "help"
}

func (*helpCmd) Description() string {
	return "Help page."
}

func (*helpCmd) SetFlags(f *flag.FlagSet) {
}

func (d *helpCmd) Execute(ctx context.Context, args []string) error {
	return flag.ErrHelp
}
