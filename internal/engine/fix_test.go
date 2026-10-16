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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestFix(t *testing.T) {
	t.Parallel()

	originalLines := []string{
		"These are",
		"the contents",
		"of the file",
		"that may be modified",
	}
	// A second file that most checks leave untouched.
	otherLines := []string{
		"other file",
	}

	data := []struct {
		name string
		want []string
		// wantOther is the expected content of other.txt; nil means unchanged.
		wantOther []string
		// wantErr is the expected error message; empty means Fix() must
		// succeed.
		wantErr string
		level   Level
	}{
		{
			name: "delete_lines.star",
			want: []string{
				"These are",
				"that may be modified",
			},
		},
		{
			name: "ignored_findings.star",
			want: originalLines,
		},
		{

			name: "insert_text.star",
			want: []string{
				"These are",
				"the INSERTED contents",
				"of the file",
				"that may be modified",
			},
		},
		{
			name: "multiple_replacements_one_file.star",
			want: []string{
				"<REPL1>",
				"the contents",
				"<REPL2>",
			},
		},
		{
			// Like overlapping_findings_converge.star, plus a tracer check
			// that appends a line to other.txt every time it runs. Only the
			// check with the skipped finding is re-run in the second pass,
			// so the tracer must run exactly once.
			name: "overlapping_findings_allowlist.star",
			want: []string{
				"THESE ARE",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
			wantOther: append(slices.Clone(otherLines), "TRACER"),
		},
		{
			// Two checks: a whole-file formatter and a line-range fixer. The
			// line-range finding overlaps with the whole-file finding so it
			// can't be applied in the same pass; Fix() should re-run the
			// checks and apply it in a second pass.
			name: "overlapping_findings_converge.star",
			want: []string{
				"THESE ARE",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
		},
		{
			// Like overlapping_findings_converge.star, except that the
			// line-range fixer records the affected .txt files it saw in the
			// pass that applied it. No files are specified here, so the
			// second pass must still see all of them; see TestFix_NarrowsFiles
			// for the narrowed case.
			name: "overlapping_findings_files.star",
			want: []string{
				"THESE ARE",
				"the contents",
				"UPDATED file.txt other.txt",
				"that may be modified",
			},
		},
		{
			// Three checks whose skipped findings shrink from pass to pass:
			// the second pass writes the same bytes as the first, but only
			// one check is left to re-run, and the third pass applies it.
			// The repeated write must not be mistaken for a cycle.
			name: "overlapping_findings_narrowing_converges.star",
			want: []string{
				"These are",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
		},
		{
			// A check whose fixes never converge: each pass appends one
			// "PASS" line and leaves one overlapping finding unapplied. The
			// file changes every pass, so Fix() must give up after
			// maxFixPasses passes and fail.
			name: "overlapping_findings_never_converge.star",
			want: append(
				slices.Clone(originalLines),
				slices.Repeat([]string{"PASS"}, maxFixPasses)...,
			),
			wantErr: "1 finding still not fixed after 5 passes due to overlap with other fixes",
		},
		{
			// A check whose whole-file replacement is a no-op and leaves one
			// overlapping finding unapplied. The second pass leaves the file
			// as the first one did, so Fix() must fail right after it rather
			// than running out maxFixPasses.
			name:    "overlapping_findings_noop.star",
			want:    originalLines,
			wantErr: "1 finding not fixed: fixes did not converge after 2 passes",
		},
		{
			// A check whose whole-file replacement toggles the first line
			// back and forth and leaves one overlapping finding unapplied.
			// The third pass leaves the file as the first one did, so Fix()
			// must fail right after it rather than running out maxFixPasses.
			name: "overlapping_findings_oscillate.star",
			want: []string{
				"THESE ARE",
				"the contents",
				"of the file",
				"that may be modified",
			},
			wantErr: "1 finding not fixed: fixes did not converge after 3 passes",
		},
		{
			// Overlapping findings in file.txt plus a single non-overlapping
			// finding in other.txt. other.txt must be fixed in the first pass
			// (the check records the pass number), and file.txt must still
			// converge.
			name: "overlapping_findings_two_files.star",
			want: []string{
				"THESE ARE",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
			wantOther: append(slices.Clone(otherLines), "FIXED IN PASS 1"),
		},
		{
			name: "replace_entire_file.star",
			want: []string{
				"this text is a replacement",
				"for the entire file",
			},
		},
		{
			name: "replace_entire_file_others_ignored.star",
			want: []string{
				"this text is a replacement",
				"for the entire file",
			},
		},
		{
			name: "replace_one_full_line.star",
			want: []string{
				"These are",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
		},
		{
			name: "replace_partial_line.star",
			want: []string{
				"These are",
				"the contents",
				"of UPDATED file",
				"that may be modified",
			},
		},
		{
			name:  "various_level_findings.star",
			level: Notice,
			want: []string{
				"NOTICE",
				"WARNING",
				"ERROR",
				"that may be modified",
			},
		},
	}
	want := make([]string, len(data))
	for i := range data {
		want[i] = data[i].name
	}
	_, got := enumDir(t, "fix")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	for i := range data {
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			m, err := filepath.Glob(filepath.Join("testdata", "fix", "*"))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, root, "file.txt", strings.Join(originalLines, "\n")+"\n")
			writeFile(t, root, "other.txt", strings.Join(otherLines, "\n")+"\n")
			for _, src := range m {
				copyFile(t, root, src)
			}

			o := Options{
				Dir:        root,
				EntryPoint: data[i].name,
				FixLevel:   data[i].level,
				config:     "../config/valid.textproto",
			}
			err = Fix(t.Context(), &o, true, nil)
			if data[i].wantErr != "" {
				if err == nil {
					t.Fatalf("Expected error: %q", data[i].wantErr)
				} else if err.Error() != data[i].wantErr {
					t.Fatalf("Expected error %q, got %q", data[i].wantErr, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			checkOptionsRestored(t, &o, nil, nil)
			got := strings.Split(readFile(t, filepath.Join(root, "file.txt")), "\n")
			want := append(data[i].want, "")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("Wrong updated file lines (-want +got):\n%s", diff)
			}
			gotOther := strings.Split(readFile(t, filepath.Join(root, "other.txt")), "\n")
			wantOther := append(slices.Clone(otherLines), "")
			if data[i].wantOther != nil {
				wantOther = append(data[i].wantOther, "")
			}
			if diff := cmp.Diff(wantOther, gotOther); diff != "" {
				t.Fatalf("Wrong updated other.txt lines (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFix_FixLevel verifies that only findings at or above the fix level are
// fixed.
func TestFix_FixLevel(t *testing.T) {
	t.Parallel()
	data := []struct {
		level Level
		want  []string
	}{
		{Nothing, []string{"NOTICE", "WARNING", "ERROR", "that may be modified", ""}},
		{Error, []string{"These are", "the contents", "ERROR", "that may be modified", ""}},
		{Warning, []string{"These are", "WARNING", "ERROR", "that may be modified", ""}},
	}
	for _, d := range data {
		t.Run(string(d.level), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "file.txt", "These are\nthe contents\nof the file\nthat may be modified\n")
			copyFile(t, root, filepath.Join("testdata", "fix", "various_level_findings.star"))
			o := Options{
				Dir:        root,
				EntryPoint: "various_level_findings.star",
				FixLevel:   d.level,
				config:     "../config/valid.textproto",
			}
			if err := Fix(t.Context(), &o, true, nil); err != nil {
				t.Fatal(err)
			}
			got := strings.Split(readFile(t, filepath.Join(root, "file.txt")), "\n")
			if diff := cmp.Diff(d.want, got); diff != "" {
				t.Fatalf("Wrong updated file lines (-want +got):\n%s", diff)
			}
		})
	}
}

// reportCompletions records findings and how many times each check completed.
type reportCompletions struct {
	reportEmitNoPrint
	completions map[string]int
}

func (r *reportCompletions) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, l Level, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.completions == nil {
		r.completions = map[string]int{}
	}
	r.completions[check]++
}

func TestFix_Report(t *testing.T) {
	t.Parallel()

	data := []struct {
		name string
		// star is the content of shac.star.
		star      string
		fixLevel  Level
		failLevel Level
		// want is the expected content of file.txt after fixing.
		want string
		// wantFindings are the reported findings, as "<check>/<level>:<line>".
		wantFindings []string
		wantErr      error
	}{
		{
			// Nothing to fix, so the findings of the first and only pass are
			// reported.
			name: "nothing_to_fix",
			star: "" +
				"def unfixable(ctx):\n" +
				"    ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = 2, message = \"bad\")\n" +
				"\n" +
				"shac.register_check(unfixable)\n",
			want:         "a\nb\n",
			wantFindings: []string{"unfixable/error:2"},
			wantErr:      ErrCheckFailed,
		},
		{
			// The fixed error is not reported, but the unfixable error is, at
			// its line after the fix inserted a line above it.
			name: "remaining_error_after_fix",
			star: "" +
				"def header(ctx):\n" +
				"    if not str(ctx.io.read_file(\"file.txt\")).startswith(\"HEADER\"):\n" +
				"        ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = 1, message = \"no header\", replacements = [\"HEADER\\na\\n\"])\n" +
				"\n" +
				"def unfixable(ctx):\n" +
				"    lines = str(ctx.io.read_file(\"file.txt\")).splitlines()\n" +
				"    ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = lines.index(\"b\") + 1, message = \"bad\")\n" +
				"\n" +
				"shac.register_check(header)\n" +
				"shac.register_check(unfixable)\n",
			want:         "HEADER\na\nb\n",
			wantFindings: []string{"unfixable/error:3"},
			wantErr:      ErrCheckFailed,
		},
		{
			// header's fix introduces trailing whitespace, which trailing_ws
			// didn't report in the first pass. It must still be fixed, and
			// nothing reported.
			name: "fix_causes_new_finding",
			star: "" +
				"def header(ctx):\n" +
				"    if not str(ctx.io.read_file(\"file.txt\")).startswith(\"HEADER\"):\n" +
				"        ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = 1, message = \"no header\", replacements = [\"HEADER  \\na\\n\"])\n" +
				"\n" +
				"def trailing_ws(ctx):\n" +
				"    for i, l in enumerate(str(ctx.io.read_file(\"file.txt\")).split(\"\\n\")):\n" +
				"        if l.endswith(\" \"):\n" +
				"            ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = i + 1, message = \"trailing ws\", replacements = [l.rstrip() + \"\\n\"])\n" +
				"\n" +
				"shac.register_check(header)\n" +
				"shac.register_check(trailing_ws)\n",
			want: "HEADER\na\nb\n",
		},
		{
			// Warnings below the fix level aren't fixed, but they are reported
			// without failing.
			name: "warning_not_fixed",
			star: "" +
				"def warn(ctx):\n" +
				"    ctx.emit.finding(level = \"warning\", filepath = \"file.txt\", line = 1, message = \"meh\", replacements = [\"A\"])\n" +
				"\n" +
				"shac.register_check(warn)\n",
			fixLevel:     Error,
			want:         "a\nb\n",
			wantFindings: []string{"warn/warning:1"},
		},
		{
			// Remaining warnings fail with a warning fail level.
			name: "warning_not_fixed_fails",
			star: "" +
				"def warn(ctx):\n" +
				"    ctx.emit.finding(level = \"warning\", filepath = \"file.txt\", line = 1, message = \"meh\", replacements = [\"A\"])\n" +
				"\n" +
				"shac.register_check(warn)\n",
			fixLevel:     Error,
			failLevel:    Warning,
			want:         "a\nb\n",
			wantFindings: []string{"warn/warning:1"},
			wantErr:      ErrCheckFailed,
		},
		{
			name: "warning_fixed",
			star: "" +
				"def warn(ctx):\n" +
				"    if str(ctx.io.read_file(\"file.txt\")).startswith(\"a\"):\n" +
				"        ctx.emit.finding(level = \"warning\", filepath = \"file.txt\", line = 1, message = \"meh\", replacements = [\"A\\n\"])\n" +
				"\n" +
				"shac.register_check(warn)\n",
			want: "A\nb\n",
		},
	}
	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "file.txt", "a\nb\n")
			writeFile(t, root, "shac.star", d.star)
			r := &reportCompletions{t: t}
			o := Options{
				Report:    r,
				Dir:       root,
				FixLevel:  d.fixLevel,
				FailLevel: d.failLevel,
				config:    "../config/valid.textproto",
			}
			if err := Fix(t.Context(), &o, true, nil); !errors.Is(err, d.wantErr) {
				t.Fatalf("Fix() error = %v, want %v", err, d.wantErr)
			}
			if o.Report != r {
				t.Errorf("Fix() did not restore o.Report")
			}
			if got := readFile(t, filepath.Join(root, "file.txt")); got != d.want {
				t.Errorf("file.txt = %q, want %q", got, d.want)
			}
			var gotFindings []string
			for _, f := range r.findings {
				gotFindings = append(gotFindings, fmt.Sprintf("%s/%s:%d", f.Check, f.Level, f.Span.Start.Line))
			}
			if diff := cmp.Diff(d.wantFindings, gotFindings); diff != "" {
				t.Errorf("Wrong findings (-want +got):\n%s", diff)
			}
			// Every check's results are reported exactly once.
			for check, n := range r.completions {
				if n != 1 {
					t.Errorf("check %s completed %d times, want 1", check, n)
				}
			}
		})
	}
}

// TestFix_RunCounts verifies how many times each check runs, since checks can
// be slow.
func TestFix_RunCounts(t *testing.T) {
	t.Parallel()

	fmtImpl := "" +
		"def fmt(ctx):\n" +
		"    for i, l in enumerate(str(ctx.io.read_file(\"file.txt\")).split(\"\\n\")):\n" +
		"        if l != l.rstrip():\n" +
		"            ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = i + 1, message = \"ws\", replacements = [l.rstrip() + \"\\n\"])\n"
	data := []struct {
		name string
		// file is the initial content of file.txt.
		file string
		// noFormatter omits the "fmt" formatter check.
		noFormatter bool
		// other is the implementation of the "other" check, a non-formatter.
		// It's omitted if empty.
		other string
		want  map[string]int
	}{
		{
			name: "only_formatter",
			file: "a  \n",
			want: map[string]int{"fmt": 2},
		},
		{
			name:        "only_other",
			file:        "a\n",
			noFormatter: true,
			other: "" +
				"    if not str(ctx.io.read_file(\"file.txt\")).startswith(\"H\"):\n" +
				"        ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = 1, message = \"h\", replacements = [\"H\\na\\n\"])\n",
			// Once to fix, and once more to confirm.
			want: map[string]int{"other": 2},
		},
		{
			name:  "nothing_to_fix",
			file:  "a\n",
			other: "    pass\n",
			want:  map[string]int{"fmt": 1, "other": 1},
		},
		{
			// The formatter runs again to confirm its fix, but the other
			// check only runs on the formatted file.
			name:  "formatting_only",
			file:  "a  \n",
			other: "    pass\n",
			want:  map[string]int{"fmt": 2, "other": 1},
		},
		{
			// The other check's fix adds trailing whitespace, so all the
			// checks run again after it.
			name: "other_fix",
			file: "a\n",
			other: "" +
				"    if not str(ctx.io.read_file(\"file.txt\")).startswith(\"H\"):\n" +
				"        ctx.emit.finding(level = \"error\", filepath = \"file.txt\", line = 1, message = \"h\", replacements = [\"H  \\na\\n\"])\n",
			want: map[string]int{"fmt": 3, "other": 3},
		},
	}
	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "file.txt", d.file)
			star := ""
			if !d.noFormatter {
				star += fmtImpl + "shac.register_check(shac.check(fmt, formatter = True))\n"
			}
			if d.other != "" {
				star += "def other(ctx):\n" + d.other + "shac.register_check(other)\n"
			}
			writeFile(t, root, "shac.star", star)
			var mu sync.Mutex
			got := map[string]int{}
			o := Options{
				Report: &reportCompletions{t: t},
				Dir:    root,
				config: "../config/valid.textproto",
				checkStarted: func(name string) {
					mu.Lock()
					defer mu.Unlock()
					got[name]++
				},
			}
			if err := Fix(t.Context(), &o, true, nil); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(d.want, got); diff != "" {
				t.Errorf("Wrong check run counts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFix_ReportWithWriter(t *testing.T) {
	t.Parallel()
	o := Options{Report: &reportNoPrint{t: t}, Dir: t.TempDir()}
	var b strings.Builder
	err := Fix(t.Context(), &o, true, &b)
	if err == nil || err.Error() != "cannot report findings when writing fixed contents" {
		t.Fatalf("Fix() error = %v", err)
	}
}

// TestFix_NarrowsFiles verifies that when files are specified, re-run passes
// only analyze the files that had skipped findings, and that the options are
// restored afterwards even when an allowlist was given.
func TestFix_NarrowsFiles(t *testing.T) {
	t.Parallel()

	originalLines := []string{
		"These are",
		"the contents",
		"of the file",
		"that may be modified",
	}

	root := t.TempDir()
	writeFile(t, root, "file.txt", strings.Join(originalLines, "\n")+"\n")
	writeFile(t, root, "other.txt", "other file\n")
	copyFile(t, root, filepath.Join("testdata", "fix", "overlapping_findings_files.star"))

	files := []string{filepath.Join(root, "file.txt"), filepath.Join(root, "other.txt")}
	allowList := []string{"one_line", "whole_file"}
	o := Options{
		Dir:        root,
		EntryPoint: "overlapping_findings_files.star",
		Files:      files,
		Filter:     CheckFilter{AllowList: allowList},
		config:     "../config/valid.textproto",
	}
	if err := Fix(t.Context(), &o, true, nil); err != nil {
		t.Fatal(err)
	}
	checkOptionsRestored(t, &o, files, allowList)

	// Only file.txt had a skipped finding, so it must be the only affected
	// file in the second pass, when the line-range finding is applied.
	got := strings.Split(readFile(t, filepath.Join(root, "file.txt")), "\n")
	want := []string{
		"THESE ARE",
		"the contents",
		"UPDATED file.txt",
		"that may be modified",
		"",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Wrong updated file lines (-want +got):\n%s", diff)
	}
}

func TestFix_ErrorRestoresOptions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "file.txt", "content\n")
	files := []string{filepath.Join(root, "file.txt")}
	allowList := []string{"some_check"}
	o := Options{
		Dir:        root,
		EntryPoint: "nonexistent.star",
		Files:      files,
		Filter:     CheckFilter{AllowList: allowList},
		config:     "../config/valid.textproto",
	}
	if err := Fix(t.Context(), &o, true, nil); err == nil {
		t.Fatal("expected an error from Fix() with a missing entrypoint")
	}
	checkOptionsRestored(t, &o, files, allowList)
}

// checkOptionsRestored verifies that Fix() left o.Files, o.Filter.AllowList
// and o.Report as they were before the call, since it modifies them while
// running the checks.
func checkOptionsRestored(t *testing.T, o *Options, wantFiles, wantAllowList []string) {
	t.Helper()
	if o.Report != nil {
		t.Errorf("Fix() did not reset o.Report")
	}
	if diff := cmp.Diff(wantFiles, o.Files); diff != "" {
		t.Errorf("Fix() modified o.Files (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantAllowList, o.Filter.AllowList); diff != "" {
		t.Errorf("Fix() modified o.Filter.AllowList (-want +got):\n%s", diff)
	}
}

func TestFixWithWriter(t *testing.T) {
	t.Parallel()

	originalLines := []string{
		"These are",
		"the contents",
		"of the file",
		"that may be modified",
	}

	root := t.TempDir()
	writeFile(t, root, "file.txt", strings.Join(originalLines, "\n")+"\n")
	copyFile(t, root, filepath.Join("testdata", "fix", "replace_one_full_line.star"))

	o := Options{
		Dir:        root,
		EntryPoint: "replace_one_full_line.star",
		config:     "../config/valid.textproto",
	}
	var b strings.Builder
	if err := Fix(t.Context(), &o, true, &b); err != nil {
		t.Fatal(err)
	}
	checkOptionsRestored(t, &o, nil, nil)

	// Verify that the file on disk was NOT modified.
	gotFile := strings.Split(readFile(t, filepath.Join(root, "file.txt")), "\n")
	wantFile := append(originalLines, "")
	if diff := cmp.Diff(wantFile, gotFile); diff != "" {
		t.Errorf("File on disk was modified but should not have been (-want +got):\n%s", diff)
	}

	// Verify that the writer contains the modified contents.
	gotWriter := strings.Split(b.String(), "\n")
	wantWriter := []string{
		"These are",
		"the contents",
		"UPDATED",
		"that may be modified",
		"",
	}
	if diff := cmp.Diff(wantWriter, gotWriter); diff != "" {
		t.Errorf("Writer contains wrong content (-want +got):\n%s", diff)
	}
}

// Not parallel and must not call t.Parallel(): it temporarily redirects
// os.Stderr to capture the warning, which would race with any test running
// concurrently.
func TestFixWithWriter_Overlap(t *testing.T) {
	originalLines := []string{
		"These are",
		"the contents",
		"of the file",
		"that may be modified",
	}

	root := t.TempDir()
	writeFile(t, root, "file.txt", strings.Join(originalLines, "\n")+"\n")
	copyFile(t, root, filepath.Join("testdata", "fix", "overlapping_findings_converge.star"))

	stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stderr = origStderr
		stderr.Close()
	})

	o := Options{
		Dir:        root,
		EntryPoint: "overlapping_findings_converge.star",
		config:     "../config/valid.textproto",
	}
	var b strings.Builder
	if err := Fix(t.Context(), &o, true, &b); err != nil {
		t.Fatal(err)
	}
	os.Stderr = origStderr
	if err := stderr.Sync(); err != nil {
		t.Fatal(err)
	}

	// Verify that the file on disk was NOT modified.
	gotFile := strings.Split(readFile(t, filepath.Join(root, "file.txt")), "\n")
	wantFile := append(originalLines, "")
	if diff := cmp.Diff(wantFile, gotFile); diff != "" {
		t.Errorf("File on disk was modified but should not have been (-want +got):\n%s", diff)
	}

	// Only the whole-file replacement is applied: with a writer the checks
	// are not re-run, since they would see the unmodified file. The output
	// must be written exactly once.
	gotWriter := strings.Split(b.String(), "\n")
	wantWriter := []string{
		"THESE ARE",
		"the contents",
		"of the file",
		"that may be modified",
		"",
	}
	if diff := cmp.Diff(wantWriter, gotWriter); diff != "" {
		t.Errorf("Writer contains wrong content (-want +got):\n%s", diff)
	}

	// The skipped finding is reported even in quiet mode.
	wantStderr := "1 finding not applied due to overlap with other fixes; apply the emitted fixes and run again\n"
	if diff := cmp.Diff(wantStderr, readFile(t, stderr.Name())); diff != "" {
		t.Errorf("Wrong stderr (-want +got):\n%s", diff)
	}
}

func TestApplyReplacements_Bounds(t *testing.T) {
	t.Parallel()
	content := "one\ntwo\n"
	tests := []struct {
		name    string
		span    Span
		want    string
		wantErr string
	}{
		{
			name:    "line beyond end of file",
			span:    Span{Start: Cursor{Line: 5}, End: Cursor{Line: 5}},
			wantErr: `check "alice" emitted finding with span (lines 5-5) beyond end of file (2 lines)`,
		},
		{
			name:    "line beyond end of file without end line",
			span:    Span{Start: Cursor{Line: 5}},
			wantErr: `check "alice" emitted finding with span (lines 5-5) beyond end of file (2 lines)`,
		},
		{
			name:    "column beyond end of line",
			span:    Span{Start: Cursor{Line: 1, Col: 10}, End: Cursor{Line: 1, Col: 11}},
			wantErr: `check "alice" emitted finding with column out of bounds on lines 1-1`,
		},
		{
			name: "insertion at end of file",
			span: Span{Start: Cursor{Line: 3, Col: 1}},
			want: "one\ntwo\nx",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := applyReplacements(content, []findingToFix{
				{check: "alice", span: test.span, replacement: "x"},
			})
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Errorf("applyReplacements(%q, %+v) error = %v, want %q", content, test.span, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyReplacements(%q, %+v) error = %v", content, test.span, err)
			}
			if got != test.want {
				t.Errorf("applyReplacements(%q, %+v) = %q, want %q", content, test.span, got, test.want)
			}
		})
	}
}
