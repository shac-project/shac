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
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxFixPasses bounds the number of times Fix re-runs checks in order to apply
// findings that were skipped in a previous pass because they overlapped with a
// finding that was applied. Well-behaved checks converge in two passes (e.g. a
// whole-file formatter followed by a line-range fixer), and checks whose fixes
// don't make progress are detected by comparing file contents between passes,
// so this limit only exists as a safety net.
const maxFixPasses = 5

// Fix loads a main shac.star file from a root directory and runs checks defined
// in it, then applies suggested fixes to files on disk.
//
// Findings whose spans overlap are not applied together, since the later
// finding's replacement was computed against file contents that the earlier
// finding's replacement changes. Instead, only non-overlapping findings are
// applied in each pass, and the checks are re-run until nothing is skipped. An
// error is returned if the fixes don't converge: either because a pass leaves
// the fixed files in a state already seen after an earlier pass (a no-op or
// oscillating fix), or because maxFixPasses is reached.
func Fix(ctx context.Context, o *Options, quiet bool, w io.Writer) error {
	if o.Report != nil {
		return fmt.Errorf("cannot overwrite reporter")
	}
	// Re-run passes narrow the allowlist and (possibly) the files; restore
	// them so that the caller's Options are unchanged on return.
	origAllowList, origFiles := o.Filter.AllowList, o.Files
	defer func() {
		o.Report = nil
		o.Filter.AllowList = origAllowList
		o.Files = origFiles
	}()

	// state holds the digest of every file fixed so far, keyed by absolute
	// path; files that were never fixed are unchanged since the first pass.
	// seen holds the fingerprint of the input of every re-run pass so far:
	// the on-disk state plus the checks and files the pass is narrowed to.
	state := map[string][sha256.Size]byte{}
	seen := map[[sha256.Size]byte]struct{}{}
	for pass := 1; ; pass++ {
		res, err := fixOnce(ctx, o, quiet, pass > 1, w)
		if err != nil {
			return err
		}
		if res.numSkipped == 0 {
			return nil
		}
		noun := "finding"
		if res.numSkipped != 1 {
			noun += "s"
		}
		if w != nil {
			// When writing to w instead of modifying files in place, re-running
			// the checks would see the unmodified files and produce the same
			// findings, so there's no point in doing another pass. This is a
			// limitation of the mode rather than a misbehaving check, so warn
			// (even in quiet mode, since silently leaving findings unfixed is
			// worse than a bit of noise) instead of failing.
			fmt.Fprintf(os.Stderr, "%d %s not applied due to overlap with other fixes; apply the emitted fixes and run again\n", res.numSkipped, noun)
			return nil
		}
		// Only the checks that had findings skipped need to run again; the
		// checks whose findings were all applied have nothing left to do.
		// Every skipped check ran in this pass, so it is allowed by the
		// original filter and the narrowed allowlist is a subset of it.
		nextChecks := res.skippedChecks
		// Similarly only the files with skipped findings need to be analyzed
		// again, but only narrow o.Files when the caller already specified
		// files, so that every pass runs in the same mode. Switching from git
		// mode to files mode would change what the checks see:
		// affected_files() reports action == "" for specified files, and the
		// ignore list from shac.textproto is not applied to them.
		nextFiles := origFiles
		if len(origFiles) > 0 {
			nextFiles = res.skippedFiles
		}
		// The next pass is fully determined by the on-disk state and what it
		// is narrowed to, so if that input was already seen, the pass will
		// produce the same output as before and the fixes will never
		// converge: a no-op replacement repeats the previous input and
		// oscillating replacements repeat an older one. Stop right away
		// rather than running out the pass bound.
		maps.Copy(state, res.written)
		fp := rerunFingerprint(state, nextChecks, nextFiles)
		if _, ok := seen[fp]; ok {
			return fmt.Errorf("%d %s not fixed: fixes did not converge after %d passes", res.numSkipped, noun, pass)
		}
		seen[fp] = struct{}{}
		if pass >= maxFixPasses {
			return fmt.Errorf("%d %s still not fixed after %d passes due to overlap with other fixes", res.numSkipped, noun, pass)
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "%d %s not yet fixed due to overlap with applied fixes; re-running checks (pass %d)\n", res.numSkipped, noun, pass+1)
		}
		o.Filter.AllowList = nextChecks
		o.Files = nextFiles
	}
}

// rerunFingerprint hashes the input of a re-run pass: the digests of the files
// fixed so far, keyed by absolute path, and the checks and files the pass is
// narrowed to. Each element is NUL-terminated and the sections are separated
// by an empty element so that different inputs can't hash the same.
func rerunFingerprint(state map[string][sha256.Size]byte, checks, files []string) [sha256.Size]byte {
	h := sha256.New()
	for _, path := range slices.Sorted(maps.Keys(state)) {
		digest := state[path]
		h.Write([]byte(path + "\x00"))
		h.Write(digest[:])
	}
	h.Write([]byte{0})
	for _, check := range checks {
		h.Write([]byte(check + "\x00"))
	}
	h.Write([]byte{0})
	for _, file := range files {
		h.Write([]byte(file + "\x00"))
	}
	var fp [sha256.Size]byte
	h.Sum(fp[:0])
	return fp
}

// passResult summarizes one pass of fixOnce.
type passResult struct {
	// numSkipped is the number of findings that were not applied because they
	// overlapped with an applied finding.
	numSkipped int
	// skippedChecks are the names of the checks with skipped findings, sorted.
	skippedChecks []string
	// skippedFiles are the absolute paths of the files with skipped findings,
	// sorted.
	skippedFiles []string
	// written holds the digest of the contents of each file written in this
	// pass, keyed by absolute path. It is unused when writing to w instead of
	// the files, since the checks are not re-run then.
	written map[string][sha256.Size]byte
}

// fixOnce runs the checks once and applies all non-overlapping findings. rerun
// indicates that this is not the first pass, in which case checks that have
// nothing left to fix are not logged, to avoid repeating the same output for
// every pass.
func fixOnce(ctx context.Context, o *Options, quiet, rerun bool, w io.Writer) (passResult, error) {
	var res passResult
	fc := findingCollector{
		countsByCheck:  map[string]int{},
		quiet:          quiet,
		rerun:          rerun,
		findingsByFile: make(map[findingFile][]findingToFix),
	}
	o.Report = &fc
	if err := Run(ctx, o); err != nil && !errors.Is(err, ErrCheckFailed) {
		return res, err
	}

	orderedFiles := make([]findingFile, 0, len(fc.findingsByFile))
	for f := range fc.findingsByFile {
		orderedFiles = append(orderedFiles, f)
	}
	// Sort for determinism.
	slices.SortFunc(orderedFiles, func(a, b findingFile) int {
		return cmp.Compare(a.path, b.path)
	})

	if len(orderedFiles) == 0 && len(o.Files) == 1 && w != nil {
		// In this scenario, there are no fixes to perform, there is only one
		// file being requested to format, and we are being requested to
		// write the files contents out. Just emit the file right back out.
		b, err := os.ReadFile(o.Files[0])
		if err != nil {
			return res, err
		}
		_, err = w.Write(b)
		return res, err
	}

	skippedChecks := map[string]struct{}{}
	res.written = make(map[string][sha256.Size]byte, len(orderedFiles))
	for _, f := range orderedFiles {
		path := filepath.Join(f.root, f.path)
		fr, err := fixFindings(path, fc.findingsByFile[f], w)
		if err != nil {
			return res, err
		}
		res.written[path] = fr.digest
		if fr.numSkipped > 0 {
			res.numSkipped += fr.numSkipped
			res.skippedFiles = append(res.skippedFiles, path)
			for _, check := range fr.skippedChecks {
				skippedChecks[check] = struct{}{}
			}
		}
		noun := "issue"
		if fr.numFixed != 1 {
			noun += "s"
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "Fixed %d %s in %s\n", fr.numFixed, noun, f.path)
		}
	}
	res.skippedChecks = slices.Sorted(maps.Keys(skippedChecks))
	slices.Sort(res.skippedFiles)
	return res, nil
}

// fileFixResult summarizes the application of findings to one file by
// fixFindings.
type fileFixResult struct {
	// numFixed is the number of findings that were applied.
	numFixed int
	// numSkipped is the number of findings that were not applied because they
	// overlapped with an applied finding.
	numSkipped int
	// skippedChecks are the names of the checks that emitted the skipped
	// findings, possibly with duplicates.
	skippedChecks []string
	// digest is a hash of the file contents after applying the findings.
	digest [sha256.Size]byte
}

// fixFindings applies the given findings to the file at path, writing the
// result to w if non-nil or back to the file otherwise.
func fixFindings(path string, findings []findingToFix, w io.Writer) (fileFixResult, error) {
	var res fileFixResult
	fi, err := os.Stat(path)
	if err != nil {
		return res, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return res, err
	}

	lines := strings.SplitAfter(string(b), "\n")

	// Sort findings by start position in order to skip findings that overlap
	// with previous ones.
	// TODO(erahm): Break ties deterministically (e.g. by check name or
	// registration order) so that overlapping findings from different checks
	// are applied in a stable order rather than goroutine completion order.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].span.Start.Line < findings[j].span.Start.Line
	})

	var normalized []findingToFix
	maxLine := 0
	for _, finding := range findings {
		finding.normalize(lines)
		// TODO(olivernewman): Return an error if span is beyond the end of the
		// file or if start/end points go beyond end of line.

		// Skip fixing any findings that overlap with previous findings. We
		// could theoretically fix multiple findings on the same line as long as
		// their column ranges don't overlap, but such changes are much more
		// likely to conflict. Skipped findings are counted so that the caller
		// can re-run the checks and apply them in a subsequent pass.
		if finding.span.Start.Line <= maxLine {
			res.numSkipped++
			res.skippedChecks = append(res.skippedChecks, finding.check)
			continue
		}

		if finding.span.End.Line > maxLine {
			maxLine = finding.span.End.Line
		}
		normalized = append(normalized, finding)
		res.numFixed++
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

	content := strings.Join(lines, "")
	res.digest = sha256.Sum256([]byte(content))
	if w != nil {
		if _, err := io.WriteString(w, content); err != nil {
			return res, err
		}
	} else {
		if err := os.WriteFile(path, []byte(content), fi.Mode()); err != nil {
			return res, err
		}
	}
	return res, nil
}

type findingFile struct {
	root string
	path string
}

type findingToFix struct {
	// check is the name of the check that emitted the finding.
	check       string
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
	mu             sync.Mutex
	findingsByFile map[findingFile][]findingToFix
	countsByCheck  map[string]int
	quiet          bool
	// rerun is true when the checks are being re-run to apply findings that
	// were skipped in a previous pass. Checks that have nothing to fix are
	// not logged in that case, since they were already reported.
	rerun bool
}

var _ Report = (*findingCollector)(nil)

func (c *findingCollector) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string, props map[string]string) error {
	// Only findings with a single replacement will be automatically fixed.
	// Findings with more than one replacement do not have a single fix that can
	// be automatically chosen.
	// TODO(olivernewman): Add level-based filtering; it should be possible to
	// only apply fixes for findings with level=error, as others are not
	// necessary to fix for `shac check` to pass.
	if len(replacements) == 1 {
		c.mu.Lock()
		defer c.mu.Unlock()
		key := findingFile{root: root, path: filepath.FromSlash(file)}
		c.findingsByFile[key] = append(c.findingsByFile[key], findingToFix{
			check:       check,
			span:        s,
			replacement: replacements[0],
		})
		c.countsByCheck[check]++
	}
	return nil
}

func (c *findingCollector) EmitCommitMessageFinding(ctx context.Context, check string, level Level, message string, commitHash string, commitMessage string, s Span, props map[string]string) error {
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
		if !c.rerun {
			c.logf("- %s (all good!)", check)
		}
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
