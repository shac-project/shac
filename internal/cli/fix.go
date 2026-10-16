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
	"context"
	"errors"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

type fixCmd struct {
	commandBase
	jsonOutput string
	exitZero   bool
	fixLevel   engine.Level
}

func (*fixCmd) Name() string {
	return "fix"
}

func (*fixCmd) Description() string {
	return "Run checks, make suggested fixes, and report the remaining findings."
}

func (c *fixCmd) SetFlags(f *flag.FlagSet) {
	c.commandBase.SetFlags(f)
	f.StringVar(&c.jsonOutput, "json-output", "", "path to write SARIF output of the remaining findings to")
	c.failLevel = engine.Error
	f.Var(&c.failLevel, "fail-level", "minimum level of remaining findings (notice, warning or error) that causes shac to fail")
	c.fixLevel = engine.Notice
	f.Var(&c.fixLevel, "fix-level", "minimum level of findings (notice, warning or error) to fix")
	f.BoolVar(&c.exitZero, "exit-zero", false, "exit 0 even if findings at or above --fail-level remain after fixing, so that a failure means shac itself failed, e.g. a check crashed or the fixes didn't converge")
}

func (c *fixCmd) Execute(ctx context.Context, files []string) error {
	err := c.runWithReport(ctx, files, c.jsonOutput, func(ctx context.Context, o *engine.Options) error {
		o.FixLevel = c.fixLevel
		return engine.Fix(ctx, o, c.quiet, nil)
	})
	if c.exitZero && errors.Is(err, engine.ErrCheckFailed) {
		return nil
	}
	return err
}
