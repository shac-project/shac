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
// Only findings with a single replacement and a level at or above o.FixLevel
// are fixed.
//
// Findings whose spans overlap are not applied together, since the later
// finding's replacement was computed against file contents that the earlier
// finding's replacement changes. Instead, only non-overlapping findings are
// applied in each pass, and the checks that had findings skipped are re-run
// until nothing is skipped. An error is returned if the fixes don't converge:
// either because a pass leaves the fixed files in a state already seen after
// an earlier pass (a no-op or oscillating fix), or because maxFixPasses is
// reached.
//
// If o.Report is set, the findings that remain once everything is fixed are
// reported to it, and ErrCheckFailed is returned if any of them is at or above
// o.FailLevel. Applying fixes can resolve or move other findings, and even cause
// new ones from checks that had none on the fixed files, so after any fix the
// checks are run again on all the files, both to fix the new findings and to
// report accurate results. To avoid running slow checks more than needed, the
// formatters are run and fixed first, like `shac fmt && shac check` would, so
// that the other checks normally run only once, on formatted files. All the
// checks are only re-run if the other checks' fixes change files.
func Fix(ctx context.Context, o *Options, quiet bool, w io.Writer) error {
	if o.FixLevel != Nothing && !o.FixLevel.isValid() {
		return fmt.Errorf("invalid fix level %q", o.FixLevel)
	}
	report := o.Report
	if report != nil && w != nil {
		return errors.New("cannot report findings when writing fixed contents")
	}
	// Emitting formatted output to w (shac fmt --emit) only supports a single
	// file, not a directory.
	if w != nil && len(o.Files) == 1 {
		if fi, err := os.Stat(o.Files[0]); err == nil && fi.IsDir() {
			return fmt.Errorf("is a directory: %s", o.Files[0])
		}
	}
	// Passes narrow the checks and (possibly) the files; restore them so that
	// the caller's Options are unchanged on return.
	origFilter, origFiles := o.Filter, o.Files
	defer func() {
		o.Report = report
		o.Filter = origFilter
		o.Files = origFiles
		o.noDirWarning = false
	}()

	lo := loopOptions{quiet: quiet, warnDirs: !quiet, record: report != nil, w: w}
	if report == nil || o.Filter.FormatterFiltering != AllChecks {
		rec, _, err := fixLoop(ctx, o, lo)
		return replay(ctx, report, err, rec)
	}

	o.Filter.FormatterFiltering = OnlyFormatters
	formatters, _, err := fixLoop(ctx, o, lo)
	noFormatters := errors.Is(err, errNoChecksToRun)
	if noFormatters {
		err = nil
	}
	if err != nil {
		return replay(ctx, report, err, formatters)
	}

	// If the other checks' fixes change files, all the checks run again below,
	// so there's no need to finish fixing in this phase.
	o.Filter.FormatterFiltering = OnlyNonFormatters
	// Without formatters, the first phase stopped before it could warn about
	// directories without affected files.
	others, changed, err := fixLoop(ctx, o, loopOptions{quiet: quiet, warnDirs: noFormatters && !quiet, record: true, stopOnChange: true})
	if errors.Is(err, errNoChecksToRun) && !noFormatters {
		err = nil
	}
	if err != nil || !changed {
		return replay(ctx, report, err, formatters, others)
	}

	// The other checks' fixes changed files, which may need formatting again
	// or otherwise affect any check's findings.
	if !quiet {
		fmt.Fprintln(os.Stderr, "re-running all checks since fixes changed files")
	}
	o.Filter.FormatterFiltering = AllChecks
	rec, _, err := fixLoop(ctx, o, loopOptions{quiet: quiet, record: true})
	return replay(ctx, report, err, rec)
}

// replay reports the output recorded by recs to report, if set, and returns
// err if set, or ErrCheckFailed if any recorded check failed.
func replay(ctx context.Context, report Report, err error, recs ...*recorder) error {
	if report == nil {
		return err
	}
	var failed bool
	for _, rec := range recs {
		if rec == nil {
			continue
		}
		if err2 := rec.replay(ctx, report); errors.Is(err2, ErrCheckFailed) {
			failed = true
		} else if err2 != nil && err == nil {
			err = err2
		}
	}
	if err == nil && failed {
		return ErrCheckFailed
	}
	return err
}

// loopOptions configures fixLoop.
type loopOptions struct {
	quiet bool
	// warnDirs enables the warning about directory arguments without affected
	// files in the first pass.
	warnDirs bool
	// record makes fixLoop run the checks again on all the files after any
	// fix, until a pass changes no file, and return the output of that last
	// pass.
	record bool
	// stopOnChange makes fixLoop return as soon as a pass changes a file,
	// without a recorder.
	stopOnChange bool
	// w receives the fixed contents instead of the files, as in Fix().
	w io.Writer
}

// fixLoop runs the checks selected by o and applies their fixes until a pass
// leaves nothing to fix, as described by Fix().
//
// If the loop fails, the returned recorder holds the output of the failed pass
// if lo.record is set and the pass ran all the checks on all the files, since
// that output is then accurate. changed reports whether any file was changed.
func fixLoop(ctx context.Context, o *Options, lo loopOptions) (rec *recorder, changed bool, err error) {
	quiet, w := lo.quiet, lo.w
	origAllowList, origFiles := o.Filter.AllowList, o.Files
	// state holds the digest of every file fixed so far, keyed by absolute
	// path; files that were never fixed are unchanged since the first pass.
	// seen holds the fingerprint of the input of every re-run pass so far:
	// the on-disk state plus the checks and files the pass is narrowed to.
	state := map[string][sha256.Size]byte{}
	seen := map[[sha256.Size]byte]struct{}{}
	// full is true if the pass runs all the checks on all the files, rather
	// than only re-running the checks that had findings skipped.
	full := true
	// fixingPasses counts the passes that applied or skipped fixes. They are
	// bounded by maxFixPasses, unlike the full pass that finds nothing left to
	// fix and only confirms the findings to report.
	fixingPasses := 0
	for pass := 1; ; pass++ {
		fc := newFindingCollector(o.fixLevel(), quiet, pass > 1)
		rec = nil
		if lo.record && full {
			// Record the output of the checks so that it can be reported if
			// this pass turns out to be the last one.
			rec = &recorder{failLevel: o.failLevel()}
			fc.tee = rec
		}
		o.noDirWarning = !lo.warnDirs || pass > 1
		res, err := fixOnce(ctx, o, fc, w)
		if err != nil {
			return rec, changed, err
		}
		changed = changed || res.changed
		if lo.stopOnChange && res.changed {
			return nil, true, nil
		}

		var nextChecks, nextFiles []string
		var msg string
		noun := "finding"
		if res.numSkipped != 1 {
			noun += "s"
		}
		if res.numSkipped == 0 {
			if !lo.record || (full && !res.changed) {
				return rec, changed, nil
			}
			nextChecks, nextFiles, full = origAllowList, origFiles, true
			msg = fmt.Sprintf("re-running checks to find remaining findings (pass %d)", pass+1)
		} else {
			if w != nil {
				// When writing to w instead of modifying files in place,
				// re-running the checks would see the unmodified files and
				// produce the same findings, so there's no point in doing
				// another pass. This is a limitation of the mode rather than
				// a misbehaving check, so warn (even in quiet mode, since
				// silently leaving findings unfixed is worse than a bit of
				// noise) instead of failing.
				fmt.Fprintf(os.Stderr, "%d %s not applied due to overlap with other fixes; apply the emitted fixes and run again\n", res.numSkipped, noun)
				return nil, changed, nil
			}
			// Only the checks that had findings skipped need to run again;
			// the checks whose findings were all applied have nothing left to
			// do. Every skipped check ran in this pass, so it is allowed by
			// the original filter and the narrowed allowlist is a subset of
			// it.
			nextChecks = res.skippedChecks
			// Similarly only the files with skipped findings need to be
			// analyzed again, but only narrow o.Files when the caller already
			// specified files, so that every pass runs in the same mode.
			// Switching from git mode to files mode would change what the
			// checks see: affected_files() reports action == "" for specified
			// files, and the ignore list from shac.textproto is not applied to
			// them.
			nextFiles = origFiles
			if len(origFiles) > 0 {
				nextFiles = res.skippedFiles
			}
			full = false
			msg = fmt.Sprintf("%d %s not yet fixed due to overlap with applied fixes; re-running checks (pass %d)", res.numSkipped, noun, pass+1)
		}
		// The next pass is fully determined by the on-disk state and what it
		// is narrowed to, so if that input was already seen, the pass will
		// produce the same output as before and the fixes will never
		// converge: a no-op replacement repeats the previous input and
		// oscillating replacements repeat an older one. Stop right away
		// rather than running out the pass bound.
		if res.changed || res.numSkipped > 0 {
			fixingPasses++
		}
		maps.Copy(state, res.written)
		fp := rerunFingerprint(state, nextChecks, nextFiles)
		_, repeated := seen[fp]
		seen[fp] = struct{}{}
		if repeated || fixingPasses >= maxFixPasses {
			if res.numSkipped == 0 {
				return nil, changed, fmt.Errorf("fixes did not converge after %d passes", pass)
			} else if repeated {
				return nil, changed, fmt.Errorf("%d %s not fixed: fixes did not converge after %d passes", res.numSkipped, noun, pass)
			}
			return nil, changed, fmt.Errorf("%d %s still not fixed after %d passes due to overlap with other fixes", res.numSkipped, noun, pass)
		}
		if !quiet {
			fmt.Fprintln(os.Stderr, msg)
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
	// changed is true if any file's contents were changed in this pass.
	changed bool
}

// fixOnce runs the checks once, collecting findings to fix with fc, and
// applies all non-overlapping findings.
func fixOnce(ctx context.Context, o *Options, fc *findingCollector, w io.Writer) (passResult, error) {
	var res passResult
	o.Report = fc
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
		res.changed = res.changed || fr.changed
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
		if !fc.quiet {
			fmt.Fprintf(os.Stderr, "Fixed %d %s in %s\n", fr.numFixed, noun, f.path)
		}
	}
	res.skippedChecks = slices.Sorted(maps.Keys(skippedChecks))
	slices.Sort(res.skippedFiles)
	return res, nil
}

// fileFixResult summarizes the application of findings to one file.
type fileFixResult struct {
	// numFixed is the number of findings that were applied.
	numFixed int
	// numSkipped is the number of findings that were not applied because they
	// overlapped with an applied finding.
	numSkipped int
	// skippedChecks are the names of the checks that emitted the skipped
	// findings, possibly with duplicates.
	skippedChecks []string
	// digest is a hash of the file contents after applying the findings. It's
	// only set by fixFindings.
	digest [sha256.Size]byte
	// changed is true if applying the findings changed the file contents.
	// It's only set by fixFindings.
	changed bool
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

	content, res, err := applyReplacements(string(b), findings)
	if err != nil {
		return res, err
	}
	res.digest = sha256.Sum256([]byte(content))
	res.changed = content != string(b)
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

// applyReplacements applies non-overlapping findings to content and returns the
// updated content along with which findings were fixed and skipped.
func applyReplacements(content string, findings []findingToFix) (string, fileFixResult, error) {
	var res fileFixResult
	lines := strings.SplitAfter(content, "\n")
	// lines ends with an empty element if content ends with a newline, which
	// doesn't count as a line of the file.
	numLines := len(lines)
	if lines[numLines-1] == "" {
		numLines--
	}

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
		// Validate before normalizing because normalize() indexes into lines.
		endLine := finding.span.End.Line
		if endLine == 0 {
			endLine = finding.span.Start.Line
		}
		// The empty element after a trailing newline is a valid line to
		// target because it allows inserting text at the end of the file.
		if finding.span.Start.Line > len(lines) || endLine > len(lines) {
			lineCount := fmt.Sprintf("%d lines", numLines)
			if numLines == 1 {
				lineCount = "1 line"
			}
			return "", fileFixResult{}, fmt.Errorf(
				"check %q emitted finding with span (lines %d-%d) beyond end of file (%s)",
				finding.check, finding.span.Start.Line, endLine, lineCount)
		}
		finding.normalize(lines)
		startLineLen := len(lines[finding.span.Start.Line-1])
		endLineLen := len(lines[finding.span.End.Line-1])
		if finding.span.Start.Col-1 > startLineLen || finding.span.End.Col-1 > endLineLen {
			return "", fileFixResult{}, fmt.Errorf(
				"check %q emitted finding with column out of bounds on lines %d-%d",
				finding.check, finding.span.Start.Line, finding.span.End.Line)
		}

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

	return strings.Join(lines, ""), res, nil
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

// findingCollector is the Report used while fixing. It collects the findings
// to fix, and forwards everything to tee, if set.
type findingCollector struct {
	mu             sync.Mutex
	findingsByFile map[findingFile][]findingToFix
	countsByCheck  map[string]int
	// minLevel is the minimum level of findings to fix.
	minLevel Level
	quiet    bool
	// rerun is true when the checks are being re-run to apply findings that
	// were skipped in a previous pass. Checks that have nothing to fix are
	// not logged in that case, since they were already reported.
	rerun bool
	// tee receives everything reported by the checks. When set, it is
	// responsible for showing the checks' output, so the collector doesn't
	// log check results or print() output itself.
	tee Report
}

func newFindingCollector(minLevel Level, quiet, rerun bool) *findingCollector {
	return &findingCollector{
		findingsByFile: map[findingFile][]findingToFix{},
		countsByCheck:  map[string]int{},
		minLevel:       minLevel,
		quiet:          quiet,
		rerun:          rerun,
	}
}

var _ Report = (*findingCollector)(nil)

func (c *findingCollector) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string, props map[string]string) error {
	if c.tee != nil {
		if err := c.tee.EmitFinding(ctx, check, level, message, root, file, s, replacements, props); err != nil {
			return err
		}
	}
	// Only findings with a single replacement will be automatically fixed.
	// Findings with more than one replacement do not have a single fix that can
	// be automatically chosen.
	if len(replacements) == 1 && level.AtLeast(c.minLevel) {
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
	if c.tee != nil {
		return c.tee.EmitCommitMessageFinding(ctx, check, level, message, commitHash, commitMessage, s, props)
	}
	return nil
}

func (c *findingCollector) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	if c.tee != nil {
		return c.tee.EmitArtifact(ctx, check, root, file, content)
	}
	return nil
}

func (c *findingCollector) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level Level, err error) {
	if c.tee != nil {
		c.tee.CheckCompleted(ctx, check, start, d, level, err)
		return
	}
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
	if c.tee != nil {
		c.tee.Print(ctx, check, file, line, message)
		return
	}
	c.logf("[%s:%d] %s", file, line, message)
}

func (c *findingCollector) logf(s string, a ...any) {
	if !c.quiet {
		fmt.Fprintf(os.Stderr, s+"\n", a...)
	}
}

// recorder is a Report that records everything reported to it so that it can
// be replayed to another Report later.
type recorder struct {
	// failLevel is the minimum level of findings that causes a check to fail.
	failLevel Level

	mu     sync.Mutex
	events []func(context.Context, Report) error
	failed bool
}

var _ Report = (*recorder)(nil)

func (r *recorder) record(f func(context.Context, Report) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, f)
}

// replay reports everything that was recorded to report, in the original
// order, and returns ErrCheckFailed if any check failed.
func (r *recorder) replay(ctx context.Context, report Report) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range r.events {
		if err := f(ctx, report); err != nil {
			return err
		}
	}
	if r.failed {
		return ErrCheckFailed
	}
	return nil
}

func (r *recorder) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string, props map[string]string) error {
	r.record(func(ctx context.Context, report Report) error {
		return report.EmitFinding(ctx, check, level, message, root, file, s, replacements, props)
	})
	return nil
}

func (r *recorder) EmitCommitMessageFinding(ctx context.Context, check string, level Level, message string, commitHash string, commitMessage string, s Span, props map[string]string) error {
	r.record(func(ctx context.Context, report Report) error {
		return report.EmitCommitMessageFinding(ctx, check, level, message, commitHash, commitMessage, s, props)
	})
	return nil
}

func (r *recorder) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	if root != "" {
		// The file may disappear once this returns, so read it now and replay
		// it as content.
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			return err
		}
		root, content = "", b
	}
	r.record(func(ctx context.Context, report Report) error {
		return report.EmitArtifact(ctx, check, root, file, content)
	})
	return nil
}

func (r *recorder) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level Level, err error) {
	r.record(func(ctx context.Context, report Report) error {
		report.CheckCompleted(ctx, check, start, d, level, err)
		return nil
	})
	if level.AtLeast(r.failLevel) {
		r.mu.Lock()
		r.failed = true
		r.mu.Unlock()
	}
}

func (r *recorder) Print(ctx context.Context, check, file string, line int, message string) {
	r.record(func(ctx context.Context, report Report) error {
		report.Print(ctx, check, file, line, message)
		return nil
	})
}
