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
	"bytes"
	"context"
	"os"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"go.fuchsia.dev/shac-project/shac/internal/reporting"
)

type commandBase struct {
	cwd        string
	allFiles   bool
	entryPoint string
	noRecurse  bool
	allowList  []string
	denyList   []string
	vars       stringMapFlag
	quiet      bool
	failLevel  engine.Level
}

func (c *commandBase) SetFlags(f *flag.FlagSet) {
	f.StringVarP(&c.cwd, "cwd", "C", ".", "directory in which to run shac")
	f.BoolVar(&c.allFiles, "all", false, "checks all files instead of only affected files")
	f.BoolVar(&c.noRecurse, "no-recurse", false, "do not look for shac.star files recursively")
	f.StringVar(&c.entryPoint, "entrypoint", engine.DefaultEntryPoint, "basename of Starlark files to run")
	f.StringSliceVar(&c.allowList, "only", nil, "comma-separated allowlist of checks to run (supports '*' globs); by default all checks are run")
	f.StringSliceVar(&c.denyList, "skip", nil, "comma-separated denylist of checks to skip (supports '*' globs); by default all checks are run")
	c.vars = stringMapFlag{}
	f.Var(&c.vars, "var", "runtime variables to set, of the form key=value")
	f.BoolVar(&c.quiet, "quiet", false, "suppress non-actionable output, such as the completion line of checks that passed")
}

func (c *commandBase) options(files []string) engine.Options {
	return engine.Options{
		Dir:        c.cwd,
		AllFiles:   c.allFiles,
		Files:      files,
		Recurse:    !c.noRecurse,
		Vars:       c.vars,
		EntryPoint: c.entryPoint,
		FailLevel:  c.failLevel,
		Filter: engine.CheckFilter{
			AllowList: c.allowList,
			DenyList:  c.denyList,
		},
	}
}

// runWithReport calls run with options whose Report reports findings to the
// user, and writes the findings as SARIF to jsonOutput if it's set.
func (c *commandBase) runWithReport(ctx context.Context, files []string, jsonOutput string, run func(context.Context, *engine.Options) error) error {
	var buf bytes.Buffer

	r, err := reporting.Get(ctx, c.quiet, c.failLevel)
	if err != nil {
		return err
	}
	r.Reporters = append(r.Reporters, &reporting.SarifReport{Out: &buf})

	o := c.options(files)
	o.Report = r

	err = run(ctx, &o)
	if err2 := r.Close(); err == nil {
		err = err2
	}

	if jsonOutput != "" {
		if err2 := os.WriteFile(jsonOutput, buf.Bytes(), 0o600); err == nil {
			err = err2
		}
	}

	return err
}
