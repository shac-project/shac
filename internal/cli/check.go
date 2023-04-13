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
	root      string
	main      string
	allFiles  bool
	noRecurse bool
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
	f.BoolVar(&c.noRecurse, "no-recurse", false, "do not look for shac.star files recursively")
}

func (c *checkCmd) Execute(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("unsupported arguments")
	}
	r, err := reporting.Get(ctx)
	if err != nil {
		return err
	}
	o := engine.Options{
		Report:   r,
		Root:     c.root,
		Main:     c.main,
		AllFiles: c.allFiles,
		Recurse:  !c.noRecurse,
	}
	err = engine.Run(ctx, &o)
	if err2 := r.Close(); err == nil {
		err = err2
	}
	return err
}
