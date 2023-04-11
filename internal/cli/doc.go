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

// Package cli is the shac CLI code.
package cli

import (
	"context"
	"errors"
	"os"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

type docCmd struct {
}

func (*docCmd) Name() string {
	return "doc"
}

func (*docCmd) Description() string {
	return "Prints out documentation for a starlark file.\nUse \"stdlib\" to print out the standard library documentation."
}

func (*docCmd) SetFlags(f *flag.FlagSet) {
}

func (d *docCmd) Execute(ctx context.Context, args []string) error {
	f := "stdlib"
	if len(args) == 1 {
		f = args[0]
	} else if len(args) > 1 {
		return errors.New("only specify one source")
	}
	doc, err := engine.Doc(f)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.WriteString(doc)
	return nil
}
