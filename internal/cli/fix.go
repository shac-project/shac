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
}

func (*fixCmd) Name() string {
	return "fix"
}

func (*fixCmd) Description() string {
	return "Run checks and make suggested fixes."
}

func (c *fixCmd) SetFlags(f *flag.FlagSet) {
	c.commandBase.SetFlags(f)
}

func (c *fixCmd) Execute(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("unsupported arguments")
	}
	o := c.options()
	return engine.Fix(ctx, &o)
}
