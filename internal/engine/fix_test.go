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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
			name: "various_level_findings.star",
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
