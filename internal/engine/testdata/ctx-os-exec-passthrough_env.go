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
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	varPrefix := os.Args[1]

	nonFileVarname := varPrefix + "VAR"
	readOnlyDirVarname := varPrefix + "RO_DIR"
	writeableDirVarname := varPrefix + "WRITEABLE_DIR"

	fmt.Printf("non-file env var: %s\n", mustGetEnv(nonFileVarname))
	fmt.Printf("read-only dir env var: %s\n", mustGetEnv(readOnlyDirVarname))
	fmt.Printf("writeable dir env var: %s\n", mustGetEnv(writeableDirVarname))

	// Make sure the writeable dir is writeable.
	if _, err := os.ReadDir(mustGetEnv(writeableDirVarname)); err != nil {
		log.Panicf("failed to read writeable dir: %s", err)
	}
	if err := os.WriteFile(filepath.Join(mustGetEnv(writeableDirVarname), "foo.txt"), []byte("hi"), 0o600); err == nil {
		fmt.Println("able to write to writeable dir")
	} else {
		log.Panicf("failed to write to writeable dir: %s", err)
	}

	if _, err := os.ReadDir(mustGetEnv(readOnlyDirVarname)); err != nil {
		log.Panicf("failed to read read-only dir: %s", err)
	}
	if err := os.WriteFile(filepath.Join(mustGetEnv(readOnlyDirVarname), "foo.txt"), []byte("hi"), 0o600); err == nil {
		fmt.Println("able to write to read-only dir")
	} else {
		fmt.Printf("error writing to read-only dir: %s\n", err)
	}
}

func mustGetEnv(name string) string {
	val, ok := os.LookupEnv(name)
	if !ok {
		log.Panicf("env var %s is not set", name)
	}
	return val
}
