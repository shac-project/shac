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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMainHelp(t *testing.T) {
	data := []struct {
		args []string
		want string
	}{
		{nil, "Usage of shac:\n"},
		{[]string{"shac"}, "Usage of shac:\n"},
		{[]string{"shac", "--help"}, "Usage of shac:\n"},
		{[]string{"shac", "check", "--help"}, "Usage of shac check:\n"},
		{[]string{"shac", "fix", "--help"}, "Usage of shac fix:\n"},
		{[]string{"shac", "fmt", "--help"}, "Usage of shac fmt:\n"},
		{[]string{"shac", "doc", "--help"}, "Usage of shac doc:\n"},
		{[]string{"shac", "version", "--help"}, "Usage of shac version:\n"},
	}
	for i, line := range data {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			b := getBuf(t)
			if Main(context.Background(), line.args) == nil {
				t.Fatal("expected error")
			}
			if s := b.String(); !strings.HasPrefix(s, line.want) {
				t.Fatalf("Got:\n%q", s)
			}
		})
	}
}

type panicWrite struct{}

func (panicWrite) Write([]byte) (int, error) {
	panic("unexpected write!")
}

func getBuf(t *testing.T) *bytes.Buffer {
	old := helpOut
	t.Cleanup(func() {
		helpOut = old
	})
	b := &bytes.Buffer{}
	helpOut = b
	return b
}

func init() {
	helpOut = panicWrite{}
	// Clear all environment variables to prevent automatic reporting mode
	// selection, which can lead to inconsistent behavior depending on the
	// environment. We cannot use os.Clearenv() since it breaks testing on
	// Windows because TEMP is necessary for tests to succeed.
	os.Unsetenv("GITHUB_RUN_ID")
	os.Unsetenv("LUCI_CONTEXT")
	os.Unsetenv("TERM")
	os.Unsetenv("VSCODE_GIT_IPC_HANDLE")
}

func TestMainErr(t *testing.T) {
	t.Parallel()

	data := map[string]func(t *testing.T) (args []string, wantErr string){
		"no shac.star files": func(t *testing.T) ([]string, string) {
			root := t.TempDir()
			return []string{"check", "-C", root, "--only", "foocheck"},
				fmt.Sprintf("no shac.star files found in %s", root)
		},
		"--all with positional arguments": func(t *testing.T) ([]string, string) {
			return []string{"check", "--all", "foo.txt", "bar.txt"},
				"--all cannot be set together with positional file arguments"
		},
		"--only flag without value": func(t *testing.T) ([]string, string) {
			root := t.TempDir()
			return []string{"check", "-C", root, "--only"},
				"flag needs an argument: --only"
		},
		"allowlist with invalid check": func(t *testing.T) ([]string, string) {
			root := t.TempDir()
			writeFile(t, root, "shac.star", "def cb(ctx): pass\nshac.register_check(cb)")
			return []string{"check", "-C", root, "--only", "does-not-exist"},
				"check does not exist: does-not-exist"
		},
		// Simple check that `shac fmt` filters out non-formatter checks.
		"fmt with no checks to run": func(t *testing.T) ([]string, string) {
			root := t.TempDir()
			writeFile(t, root, "shac.star", ""+
				"def non_formatter(ctx):\n"+
				"    pass\n"+
				"shac.register_check(shac.check(non_formatter))\n")
			return []string{"fmt", "-C", root, "--only", "non_formatter"},
				"no checks to run"
		},
		// Simple check that `shac fix` filters out formatters.
		"fix with no checks to run": func(t *testing.T) ([]string, string) {
			root := t.TempDir()
			writeFile(t, root, "shac.star", ""+
				"def formatter(ctx):\n"+
				"    pass\n"+
				"shac.register_check(shac.check(formatter, formatter = True))\n")
			return []string{"fix", "-C", root, "--only", "formatter"},
				"no checks to run"
		},
	}
	for name, f := range data {
		f := f
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args, wantErr := f(t)
			cmd := append([]string{"shac"}, args...)
			err := Main(context.Background(), cmd)
			if err == nil {
				t.Fatalf("Expected error from running %s", cmd)
			}
			if diff := cmp.Diff(wantErr, err.Error()); diff != "" {
				t.Fatalf("Wrong error (-want +got):\n%s", diff)
			}
		})
	}
}

func writeFile(t testing.TB, root, path, content string) {
	t.Helper()
	writeFileBytes(t, root, path, []byte(content), 0o600)
}

func writeFileBytes(t testing.TB, root, path string, content []byte, perm os.FileMode) {
	t.Helper()
	abs := filepath.Join(root, path)
	if err := os.WriteFile(abs, content, perm); err != nil {
		t.Fatal(err)
	}
}
