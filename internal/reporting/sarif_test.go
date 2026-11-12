// Copyright 2025 The Shac Authors
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

package reporting

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"go.fuchsia.dev/shac-project/shac/internal/sarif"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestReplacementsForDiff(t *testing.T) {
	a := strings.Split("ABCDEFGHIJLKMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", "")
	for i := range a {
		a[i] += "\n"
	}

	// Copy the original and make some modifications. Modifications are in
	// reverse order so that the line numbers here correspond (with an offset of
	// one) to the line numbers in the expected data.
	b := slices.Clone(a)

	var want []*sarif.Replacement

	// Add a new line at the end without a trailing new line.
	b = append(b, "9")
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine:   53,
			EndLine:     53,
			StartColumn: 1,
			EndColumn:   1,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "9",
		},
	})

	// Replace one line with multiple lines.
	b = slices.Replace(b, 30, 31, "2\n", "3\n", "4\n", "5\n", "6\n")
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 31,
			EndLine:   31,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "2\n3\n4\n5\n6\n",
		},
	})

	// Insert a blank line.
	b = slices.Insert(b, 24, "\n")
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 25,
			EndLine:   25,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "\nY\n",
		},
	})

	// Delete a few elements.
	b = slices.Delete(b, 13, 18)
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 14,
			EndLine:   18,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "",
		},
	})

	// Change a single line.
	b[3] = "1\n"
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 4,
			EndLine:   4,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "1\n",
		},
	})

	slices.Reverse(want)
	got := replacementsForDiff(a, b)

	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("Wrong parsed diff (-want +got):\n%s", d)
	}
}

type mockBacktraceableError struct {
	msg   string
	trace string
}

func (m mockBacktraceableError) Error() string     { return m.msg }
func (m mockBacktraceableError) Backtrace() string { return m.trace }

func TestSarifReportCheckCompletedException(t *testing.T) {
	sr := &SarifReport{}
	ctx := t.Context()
	start := time.Now()

	err := mockBacktraceableError{
		msg:   "fail: something crashed",
		trace: "Traceback:\n  file.star:10\nfail: something crashed",
	}

	sr.CheckCompleted(ctx, "my-check", start, time.Second, engine.Error, err)

	if len(sr.resultsByCheck["my-check"]) != 1 {
		t.Fatalf("expected 1 result, got %d", len(sr.resultsByCheck["my-check"]))
	}

	res := sr.resultsByCheck["my-check"][0]
	if res.Level != sarif.Error {
		t.Errorf("expected level %s, got %s", sarif.Error, res.Level)
	}
	want := err.msg + "\n\n" + err.trace
	if res.Message.Text != want {
		t.Errorf("expected message text %q, got %q", want, res.Message.Text)
	}

	// Test regular error without backtrace.
	err2 := errors.New("regular error")
	sr.CheckCompleted(ctx, "my-check2", start, time.Second, engine.Error, err2)

	res2 := sr.resultsByCheck["my-check2"][0]
	if res2.Message.Text != "regular error" {
		t.Errorf("expected message text %q, got %q", "regular error", res2.Message.Text)
	}
}

func TestSarifReportMultibyteColumns(t *testing.T) {
	t.Parallel()

	// Each emoji is 4 bytes in UTF-8 and 2 UTF-16 code units (a surrogate
	// pair).
	const fileContent = "hi 🚨 there\n🚨🔥 x\n"
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "foo.md"), []byte(fileContent), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		// If set, the finding is emitted on this commit message rather than on
		// fileContent.
		commitMessage string
		span          engine.Span
		want          *sarif.Region
	}{
		{
			// "🚨" starts after the 3 ASCII bytes of "hi ".
			name: "emoji",
			span: engine.Span{
				Start: engine.Cursor{Line: 1, Col: 4},
				End:   engine.Cursor{Line: 1, Col: 8},
			},
			want: &sarif.Region{StartLine: 1, EndLine: 1, StartColumn: 4, EndColumn: 6},
		},
		{
			// "🔥" starts after "🚨", which is 4 bytes but 2 UTF-16 code units.
			name: "emoji after another emoji",
			span: engine.Span{
				Start: engine.Cursor{Line: 2, Col: 5},
				End:   engine.Cursor{Line: 2, Col: 9},
			},
			want: &sarif.Region{StartLine: 2, EndLine: 2, StartColumn: 3, EndColumn: 5},
		},
		{
			// "x" starts after "🚨🔥 ", which is 9 bytes but 5 UTF-16 code units.
			name: "ASCII after emojis",
			span: engine.Span{
				Start: engine.Cursor{Line: 2, Col: 10},
				End:   engine.Cursor{Line: 2, Col: 11},
			},
			want: &sarif.Region{StartLine: 2, EndLine: 2, StartColumn: 6, EndColumn: 7},
		},
		{
			// "x" starts after "Body 🚨 ", which is 10 bytes but 8 UTF-16 code
			// units.
			name:          "commit message",
			commitMessage: "Subject\n\nBody 🚨 x\n",
			span: engine.Span{
				Start: engine.Cursor{Line: 3, Col: 11},
				End:   engine.Cursor{Line: 3, Col: 12},
			},
			want: &sarif.Region{StartLine: 3, EndLine: 3, StartColumn: 9, EndColumn: 10},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var buf bytes.Buffer
			sr := &SarifReport{Out: &buf}
			if tc.commitMessage != "" {
				if err := sr.EmitCommitMessageFinding(ctx, "my-check", engine.Warning, "msg", "deadbeef", tc.commitMessage, tc.span, nil); err != nil {
					t.Fatal(err)
				}
			} else if err := sr.EmitFinding(ctx, "my-check", engine.Warning, "msg", root, "foo.md", tc.span, []string{"!"}, nil); err != nil {
				t.Fatal(err)
			}
			if err := sr.Close(); err != nil {
				t.Fatal(err)
			}

			var doc sarif.Document
			if err := protojson.Unmarshal(buf.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			got := doc.Runs[0].Results[0]
			if d := cmp.Diff(tc.want, got.Locations[0].PhysicalLocation.Region, protocmp.Transform()); d != "" {
				t.Errorf("location region diff (-want +got):\n%s", d)
			}
			for _, fix := range got.Fixes {
				if d := cmp.Diff(tc.want, fix.ArtifactChanges[0].Replacements[0].DeletedRegion, protocmp.Transform()); d != "" {
					t.Errorf("fix region diff (-want +got):\n%s", d)
				}
			}
		})
	}
}
