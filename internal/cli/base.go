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
	cwd        string
	allFiles   bool
	entryPoint string
	noRecurse  bool
	allowList  []string
	denyList   []string
	vars       stringMapFlag
}

func (c *commandBase) SetFlags(f *flag.FlagSet) {
	f.StringVarP(&c.cwd, "cwd", "C", ".", "directory in which to run shac")
	f.BoolVar(&c.allFiles, "all", false, "checks all the files instead of guess the upstream to diff against")
	f.BoolVar(&c.noRecurse, "no-recurse", false, "do not look for shac.star files recursively")
	f.StringVar(&c.entryPoint, "entrypoint", engine.DefaultEntryPoint, "basename of Starlark files to run")
	f.StringSliceVar(&c.allowList, "only", nil, "comma-separated allowlist of checks to run; by default all checks are run")
	f.StringSliceVar(&c.denyList, "skip", nil, "comma-separated denylist of checks to skip; by default all checks are run")
	c.vars = stringMapFlag{}
	f.Var(&c.vars, "var", "runtime variables to set, of the form key=value")
}

func (c *commandBase) options(files []string) (engine.Options, error) {
	if c.allFiles && len(files) > 0 {
		return engine.Options{}, errors.New("--all cannot be set together with positional file arguments")
	}
	return engine.Options{
		Dir:        c.cwd,
		AllFiles:   c.allFiles,
		Files:      files,
		Recurse:    !c.noRecurse,
		Vars:       c.vars,
		EntryPoint: c.entryPoint,
		Filter: engine.CheckFilter{
			AllowList: c.allowList,
			DenyList:  c.denyList,
		},
	}, nil
}
