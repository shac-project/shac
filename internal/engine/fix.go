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
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fix loads a main shac.star file from a root directory and runs checks defined
// in it, then applies suggested fixes to files on disk.
func Fix(ctx context.Context, o *Options, quiet bool) error {
	fc := findingCollector{
		countsByCheck: map[string]int{},
		quiet:         quiet,
	}
	if o.Report != nil {
		return fmt.Errorf("cannot overwrite reporter")
	}
	o.Report = &fc
	if err := Run(ctx, o); err != nil && !errors.Is(err, ErrCheckFailed) {
		return err
	}

	findingsByFile := make(map[string][]findingToFix)
	for _, f := range fc.findings {
		findingsByFile[f.file] = append(findingsByFile[f.file], f)
	}

	orderedFiles := make([]string, 0, len(findingsByFile))
	for f := range findingsByFile {
		orderedFiles = append(orderedFiles, f)
	}
	// Sort for determinism.
	sort.Strings(orderedFiles)

	for _, path := range orderedFiles {
		findings := findingsByFile[path]
		numFixed, err := fixFindings(path, findings)
		if err != nil {
			return err
		}
		noun := "issue"
		if numFixed != 1 {
			noun += "s"
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "Fixed %d %s in %s\n", numFixed, noun, path)
		}
	}
	return nil
}

func fixFindings(path string, findings []findingToFix) (int, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	lines := strings.SplitAfter(string(b), "\n")

	// Sort findings by start position in order to skip findings that overlap
	// with previous ones.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].span.Start.Line < findings[j].span.Start.Line
	})

	var normalized []findingToFix
	numFixed := 0
	maxLine := 0
	for _, finding := range findings {
		finding.normalize(lines)
		// TODO(olivernewman): Return an error if span is beyond the end of the
		// file or if start/end points go beyond end of line.

		// Skip fixing any findings that overlap with previous findings. We
		// could theoretically fix multiple findings on the same line as long as
		// their column ranges don't overlap, but such changes are much more
		// likely to conflict.
		// TODO(olivernewman): Emit a warning that there are more findings to
		// apply. Alternatively, keep re-running checks and applying
		// non-overlapping findings until there are no findings left to apply.
		if finding.span.Start.Line <= maxLine {
			continue
		}

		if finding.span.End.Line > maxLine {
			maxLine = finding.span.End.Line
		}
		normalized = append(normalized, finding)
		numFixed++
	}

	// Reverse findings so earlier findings' line numbers won't be affected by
	// applying later findings.
	slices.Reverse(normalized)

	for _, finding := range normalized {
		replLines := strings.SplitAfter(finding.replacement, "\n")
		// Update replacement to contain entire lines so we can replace all the
		// affected lines in one go.
		replLines[0] = lines[finding.span.Start.Line-1][:finding.span.Start.Col-1] + replLines[0]
		replLines[len(replLines)-1] += lines[finding.span.End.Line-1][finding.span.End.Col-1:]

		lines = slices.Replace(
			lines,
			finding.span.Start.Line-1,
			finding.span.End.Line,
			replLines...)
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), fi.Mode()); err != nil {
		return 0, err
	}
	return numFixed, nil
}

type findingToFix struct {
	file        string
	span        Span
	replacement string
}

// normalize applies all default values to the finding's span, updating it
// in-place.
func (f *findingToFix) normalize(fileLines []string) {
	// If start_line is unset then all other span fields will also be unset, and
	// the span will defaults to the whole file.
	if f.span.Start.Line == 0 {
		f.span.Start.Line = 1
		f.span.End.Line = len(fileLines)
	}
	// end_line defaults to start_line.
	if f.span.End.Line == 0 {
		f.span.End.Line = f.span.Start.Line
	}
	// start_col defaults to 1.
	if f.span.Start.Col == 0 {
		f.span.Start.Col = 1
	}
	// end_col defaults to the last column of the ending line.
	if f.span.End.Col == 0 {
		idx := f.span.End.Line - 1
		f.span.End.Col = len(fileLines[idx]) + 1
	}
}

type findingCollector struct {
	mu            sync.Mutex
	findings      []findingToFix
	countsByCheck map[string]int
	quiet         bool
}

var _ Report = (*findingCollector)(nil)

func (c *findingCollector) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error {
	// Only findings with a single replacement will be automatically fixed.
	// Findings with more than one replacement do not have a single fix that can
	// be automatically chosen.
	// TODO(olivernewman): Add level-based filtering; it should be possible to
	// only apply fixes for findings with level=error, as others are not
	// necessary to fix for `shac check` to pass.
	if len(replacements) == 1 {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.findings = append(c.findings, findingToFix{
			file:        filepath.Join(root, filepath.FromSlash(file)),
			span:        s,
			replacement: replacements[0],
		})
		c.countsByCheck[check]++
	}
	return nil
}

func (c *findingCollector) EmitArtifact(context.Context, string, string, string, []byte) error {
	return nil
}

func (c *findingCollector) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level Level, err error) {
	c.mu.Lock()
	count := c.countsByCheck[check]
	c.mu.Unlock()

	// TODO(olivernewman): Make this output colorful and more consistent with
	// the output of `shac check`.
	if err != nil {
		c.logf("- %s: %s", check, err)
	} else if count == 0 {
		c.logf("- %s (all good!)", check)
	} else {
		noun := "finding"
		if count > 1 {
			noun += "s"
		}
		c.logf("- %s (%d %s to fix)", check, count, noun)
	}
}

func (c *findingCollector) Print(ctx context.Context, check, file string, line int, message string) {
	c.logf("[%s:%d] %s", file, line, message)
}

func (c *findingCollector) logf(s string, a ...any) {
	if !c.quiet {
		fmt.Fprintf(os.Stderr, s+"\n", a...)
	}
}
