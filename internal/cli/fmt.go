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

type fmtCmd struct {
	commandBase
}

func (*fmtCmd) Name() string {
	return "fmt"
}

func (*fmtCmd) Description() string {
	return "Auto-format files."
}

func (c *fmtCmd) SetFlags(f *flag.FlagSet) {
	c.commandBase.SetFlags(f)
}

func (c *fmtCmd) Execute(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("unsupported arguments")
	}
	o := c.options()
	o.Filter = engine.OnlyFormatters
	return engine.Fix(ctx, &o)
}
