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
	"go.fuchsia.dev/shac-project/shac/internal/reporting"
)

type checkCmd struct {
	root     string
	main     string
	allFiles bool
}

func (*checkCmd) Name() string {
	return "check"
}

func (*checkCmd) Description() string {
	return "Run checks in a file."
}

func (c *checkCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.root, "root", ".", "path to the root of the tree to analyse")
	f.StringVar(&c.main, "main", "shac.star", "main of the main shac.star")
	f.BoolVar(&c.allFiles, "all", false, "checks all the files instead of guess the upstream to diff against")
}

func (c *checkCmd) Execute(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("unsupported arguments")
	}
	return engine.Run(ctx, c.root, c.main, c.allFiles, reporting.Get())
}
