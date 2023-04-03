// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	base := filepath.Dir(filepath.Dir(cwd))
	c := exec.Command("go", "run", ".", "doc")
	c.Dir = base
	o, err := c.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "doc", "stdlib.md"), o, 0o644); err != nil {
		log.Fatal(err)
	}
}
