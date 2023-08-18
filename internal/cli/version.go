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
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

type versionCmd struct {
}

func (*versionCmd) Name() string {
	return "version"
}

func (*versionCmd) Description() string {
	return "Print shac version."
}

func (*versionCmd) SetFlags(f *flag.FlagSet) {
}

func (*versionCmd) Execute(ctx context.Context, args []string) error {
	_, err := fmt.Fprintf(os.Stdout, "shac v%d.%d.%d\n",
		engine.Version[0],
		engine.Version[1],
		engine.Version[2],
	)
	return err
}
