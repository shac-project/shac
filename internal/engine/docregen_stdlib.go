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

//go:build ignore

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	log.SetFlags(0)
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	base := filepath.Dir(filepath.Dir(cwd))
	c := exec.Command("go", "run", ".", "doc")
	c.Dir = base
	o, err := c.CombinedOutput()
	if err != nil {
		log.Fatalf("failed to run \"go run . doc\": %s\n%s", err, o)
	}
	if err := os.WriteFile(filepath.Join(base, "doc", "stdlib.md"), o, 0o644); err != nil {
		log.Fatal(err)
	}
}
