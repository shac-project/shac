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

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"go.fuchsia.dev/shac-project/shac/internal/execsupport"
)

// cachedGitEnv should never be accessed directly, only by calling `gitEnv()`.
var cachedGitEnv []string
var populateGitEnvOnce sync.Once

func gitEnv() []string {
	populateGitEnvOnce.Do(func() {
		// First is for git version before 2.32, the rest are to skip the user and system config.
		cachedGitEnv = append(os.Environ(),
			"GIT_CONFIG_NOGLOBAL=true",
			"GIT_CONFIG_GLOBAL=",
			"GIT_CONFIG_SYSTEM=",
			"LANG=C",
			"GIT_EXTERNAL_DIFF=",
			"GIT_DIFF_OPTS=",
		)
		gitConfig := map[string]string{
			// Prevents automatic unicode decomposition of filenames. Only has
			// an effect on macOS.
			"core.precomposeUnicode": "true",
		}
		cachedGitEnv = append(cachedGitEnv, gitConfigEnv(gitConfig)...)
	})
	return cachedGitEnv
}

func runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	args = append([]string{
		// Don't update the git index during read operations.
		"--no-optional-locks",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	b := buffers.get()
	cmd.Stdout = b
	cmd.Stderr = b
	err := execsupport.Run(cmd)
	// Always make a copy of the output, since it could be persisted. Only reuse
	// the temporary buffer.
	out := b.String()
	buffers.push(b)
	if err != nil {
		if errExit := (&exec.ExitError{}); errors.As(err, &errExit) {
			return "", fmt.Errorf("error running git %s: %w\n%s", strings.Join(args, " "), err, out)
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// gitConfigEnv converts a map of key-value git config pairs into corresponding
// environment variables.
//
// See https://git-scm.com/docs/git-config#ENVIRONMENT for details on how git
// configs are set via environment variables.
func gitConfigEnv(gitConfig map[string]string) []string {
	// GIT_CONFIG_COUNT specifies how many key/value env var pairs to look for.
	res := []string{fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(gitConfig))}

	keys := make([]string, 0, len(gitConfig))
	for k := range gitConfig {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i, k := range keys {
		// Each config setting is specified by setting a pair of
		// GIT_CONFIG_KEY_<N> and GIT_CONFIG_VALUE_<N> variables.
		res = append(res,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, k),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, gitConfig[k]))
	}
	return res
}
