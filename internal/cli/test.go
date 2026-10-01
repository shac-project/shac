// Copyright 2026 The Shac Authors
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

type testCmd struct {
	cwd        string
	allowList  []string
	denyList   []string
	vars       stringMapFlag
	quiet      bool
	jsonOutput string
}

func (*testCmd) Name() string {
	return "test"
}

func (*testCmd) Description() string {
	return "Run Starlark tests for checks."
}

func (c *testCmd) SetFlags(f *flag.FlagSet) {
	f.StringVarP(&c.cwd, "cwd", "C", ".", "directory in which to run shac")
	f.StringSliceVar(&c.allowList, "only", nil, "comma-separated allowlist of tests to run; by default all tests are run")
	f.StringSliceVar(&c.denyList, "skip", nil, "comma-separated denylist of tests to skip; by default all tests are run")
	c.vars = stringMapFlag{}
	f.Var(&c.vars, "var", "runtime variables to set, of the form key=value")
	f.BoolVar(&c.quiet, "quiet", false, "suppress non-actionable output, such as the completion line of tests that passed")
	f.StringVar(&c.jsonOutput, "json-output", "", "path to write JSON test results to")
}

func (c *testCmd) Execute(ctx context.Context, files []string) error {
	var out io.Writer = os.Stdout
	withColor := os.Getenv("TERM") != "dumb" && isatty.IsTerminal(os.Stderr.Fd())
	if withColor {
		out = colorable.NewColorableStdout()
	}
	rep := &cliTestReporter{
		out:       out,
		quiet:     c.quiet,
		withColor: withColor,
		results:   []testCaseResult{},
	}
	o := engine.Options{
		TestReporter: rep,
		Dir:          c.cwd,
		Files:        files,
		Vars:         c.vars,
		Filter: engine.CheckFilter{
			AllowList: c.allowList,
			DenyList:  c.denyList,
		},
	}
	err := engine.RunTests(ctx, &o)
	if c.jsonOutput != "" && (err == nil || errors.Is(err, engine.ErrCheckFailed)) {
		b, err2 := json.MarshalIndent(rep.results, "", "  ")
		if err2 == nil {
			b = append(b, '\n')
			err2 = os.WriteFile(c.jsonOutput, b, 0o600)
		}
		if err == nil {
			err = err2
		}
	}
	return err
}

type testCaseResult struct {
	Name      string        `json:"name"`
	File      string        `json:"file"`
	Duration  time.Duration `json:"duration_nanos"`
	Status    string        `json:"status"`
	Error     string        `json:"error,omitempty"`
	Backtrace string        `json:"backtrace,omitempty"`
	Prints    []string      `json:"prints,omitempty"`
}

type cliTestReporter struct {
	out       io.Writer
	quiet     bool
	withColor bool
	results   []testCaseResult
}

func (r *cliTestReporter) Print(ctx context.Context, file string, line int, msg string) {
	_, _ = fmt.Fprintf(r.out, "[%s:%d] %s\n", file, line, msg)
}

func (r *cliTestReporter) TestResult(ctx context.Context, name, file string, d time.Duration, err error, prints []string) {
	res := testCaseResult{
		Name:     strings.TrimPrefix(name, file+":"),
		File:     file,
		Duration: d,
		Status:   "PASS",
		Prints:   prints,
	}
	if err != nil {
		res.Status = "FAIL"
		res.Error = err.Error()
		if stackerr, ok := errors.AsType[engine.BacktraceableError](err); ok {
			res.Backtrace = stackerr.Backtrace()
		}
	}
	r.results = append(r.results, res)

	if r.quiet && err == nil {
		return
	}
	for _, p := range prints {
		_, _ = fmt.Fprintf(r.out, "  %s\n", p)
	}
	dur := d.Round(time.Millisecond)
	if err == nil {
		if r.withColor {
			_, _ = fmt.Fprintf(r.out, "- \x1b[32m%s\x1b[0m (passed in %s)\n", name, dur)
		} else {
			_, _ = fmt.Fprintf(r.out, "- %s (passed in %s)\n", name, dur)
		}
		return
	}
	if r.withColor {
		_, _ = fmt.Fprintf(r.out, "- \x1b[31m%s\x1b[0m (failed in %s): %s\n", name, dur, err)
	} else {
		_, _ = fmt.Fprintf(r.out, "- %s (failed in %s): %s\n", name, dur, err)
	}
	if res.Backtrace != "" {
		_, _ = io.WriteString(r.out, res.Backtrace)
	}
}
