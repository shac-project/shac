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

type checkCmd struct {
	commandBase
	jsonOutput string
}

func (*checkCmd) Name() string {
	return "check"
}

func (*checkCmd) Description() string {
	return "Run checks in a file."
}

func (c *checkCmd) SetFlags(f *flag.FlagSet) {
	c.commandBase.SetFlags(f)
	f.StringVar(&c.jsonOutput, "json-output", "", "path to write SARIF output to")
}

func (c *checkCmd) Execute(ctx context.Context, files []string) error {
	var buf bytes.Buffer

	r, err := reporting.Get(ctx)
	if err != nil {
		return err
	}
	r.Reporters = append(r.Reporters, &reporting.SarifReport{Out: &buf})

	o, err := c.options(files)
	if err != nil {
		return err
	}
	o.Report = r

	err = engine.Run(ctx, &o)
	if err2 := r.Close(); err == nil {
		err = err2
	}

	if c.jsonOutput != "" {
		if err2 := os.WriteFile(c.jsonOutput, buf.Bytes(), 0o600); err == nil {
			err = err2
		}
	}

	return err
}
