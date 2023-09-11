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
	"errors"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

type commandBase struct {
	root      string
	cwd       string
	allFiles  bool
	noRecurse bool
}

func (c *commandBase) SetFlags(f *flag.FlagSet) {
	f.StringVarP(&c.cwd, "cwd", "C", ".", "directory in which to run shac")
	f.BoolVar(&c.allFiles, "all", false, "checks all the files instead of guess the upstream to diff against")
	f.BoolVar(&c.noRecurse, "no-recurse", false, "do not look for shac.star files recursively")

	// TODO(olivernewman): Delete this flag after it's no longer used.
	f.StringVar(&c.root, "root", ".", "path to the root of the tree to analyse")
	// MarkHidden instead of MarkDeprecated to prevent warnings from being
	// emitted.
	f.MarkHidden("root")
}

func (c *commandBase) options(files []string) (engine.Options, error) {
	if c.allFiles && len(files) > 0 {
		return engine.Options{}, errors.New("--all cannot be set together with positional file arguments")
	}
	cwd := c.cwd
	if c.root != "." {
		if cwd != "." {
			return engine.Options{}, errors.New("--root and --cwd cannot both be set")
		}
		cwd = c.root
	}
	return engine.Options{
		Root:     cwd,
		AllFiles: c.allFiles,
		Files:    files,
		Recurse:  !c.noRecurse,
	}, nil
}
