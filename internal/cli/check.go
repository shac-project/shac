// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import (
	"context"
	"fmt"

	flag "github.com/spf13/pflag"
)

type checkCmd struct{}

func (*checkCmd) Name() string {
	return "check"
}

func (*checkCmd) Description() string {
	return "Run checks in a file."
}

func (*checkCmd) SetFlags(*flag.FlagSet) {
}

func (*checkCmd) Execute(context.Context, *flag.FlagSet) error {
	fmt.Println("hello world")
	return nil
}
