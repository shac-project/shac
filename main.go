// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"go.fuchsia.dev/shac-project/shac/internal/cli"
)

func main() {
	if err := cli.Main(); err != nil {
		fmt.Fprintf(os.Stderr, "shac: %s\n", err)
		os.Exit(1)
	}
}
