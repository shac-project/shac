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

package reporting

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mattn/go-colorable"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"go.fuchsia.dev/shac-project/shac/internal/sarif"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestGet(t *testing.T) {
	r, err := Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBasic(t *testing.T) {
	buf := bytes.Buffer{}
	r := basic{out: &buf}
	ctx := context.Background()
	// No context.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message1", "", "", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	// Only a file.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message2", "", "testdata/file.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	// File and line number. More than this is ignored.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message3", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "testdata/file.txt", []byte("content")); err == nil {
		t.Fatal("expected failure")
	}
	start := time.Now()
	r.CheckCompleted(ctx, "mycheck", start, time.Millisecond, engine.Notice, nil)
	r.CheckCompleted(ctx, "badcheck", start, time.Millisecond, engine.Notice, errors.New("bad"))
	r.Print(ctx, "", "src.star", 12, "debugmsg")
	r.Print(ctx, "mycheck", "src.star", 12, "debugmsg")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := "[mycheck/notice] message1\n" +
		"[mycheck/notice] testdata/file.txt: message2\n" +
		"[mycheck/notice] testdata/file.txt(10): message3\n" +
		"- mycheck (success in 1ms)\n" +
		"- badcheck (error in 1ms): bad\n" +
		"[src.star:12] debugmsg\n" +
		"- mycheck [src.star:12] debugmsg\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestGitHub(t *testing.T) {
	buf := bytes.Buffer{}
	r := github{out: &buf}
	ctx := context.Background()
	// No context.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message1", "", "", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	// Only a file.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message2", "", "testdata/file.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	// File and line number.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message3", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	// File, line number and column.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message4", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10, Col: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	// File, two line numbers.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message5", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10}, End: engine.Cursor{Line: 12}}, nil); err != nil {
		t.Fatal(err)
	}
	// file, start and end span on separate lines.
	if err := r.EmitFinding(ctx, "mycheck", engine.Notice, "message6", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10, Col: 1}, End: engine.Cursor{Line: 12, Col: 2}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "testdata/file.txt", []byte("content")); err == nil {
		t.Fatal("expected failure")
	}
	start := time.Now()
	r.CheckCompleted(ctx, "mycheck", start, time.Millisecond, engine.Notice, nil)
	r.CheckCompleted(ctx, "badcheck", start, time.Millisecond, engine.Notice, errors.New("bad"))
	r.Print(ctx, "", "src.star", 12, "debugmsg")
	r.Print(ctx, "mycheck", "src.star", 12, "debugmsg")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := "::notice ::title=mycheck::message1\n" +
		"::notice ::file=testdata/file.txttitle=mycheck::message2\n" +
		"::notice ::file=testdata/file.txt,line=10,title=mycheck::message3\n" +
		"::notice ::file=testdata/file.txt,line=10,col=1,title=mycheck::message4\n" +
		"::notice ::file=testdata/file.txt,line=10,endLine=12,title=mycheck::message5\n" +
		"::notice ::file=testdata/file.txt,line=10,col=1,endLine=12,endCol=2,title=mycheck::message6\n" +
		"::debug::[src.star:12] debugmsg\n" +
		"::debug::mycheck [src.star:12] debugmsg\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestInteractive_Finding(t *testing.T) {
	t.Parallel()
	data := []struct {
		name     string
		l        engine.Level
		filepath string
		span     engine.Span
		want     string
	}{
		{
			"no context/notice",
			engine.Notice,
			"",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] message1\n",
		},
		{
			"no context/warning",
			engine.Warning,
			"",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<Y>warning<R>] message1\n",
		},
		{
			"no context/error",
			engine.Error,
			"",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<Re>error<R>] message1\n",
		},
		{
			"file only",
			engine.Notice,
			"file.txt",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt: message1\n",
		},
		{
			"start line first",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 1}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(1): message1\n" +
				"\n" +
				"  <G>This<R>\n" +
				"  File\n" +
				"\n",
		},
		{
			"start line middle",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 3}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(3): message1\n" +
				"\n" +
				"  File\n" +
				"  <G>Has<R>\n" +
				"  A\n" +
				"\n",
		},
		{
			"start line last",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 9}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(9): message1\n" +
				"\n" +
				"  Some\n" +
				"  <G>Content<R>\n" +
				"  \n" +
				"\n",
		},
		{
			"start line past end",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 10}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(10): message1\n",
		},
		{
			"end line same",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 3}, End: engine.Cursor{Line: 3}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(3): message1\n" +
				"\n" +
				"  File\n" +
				"  <G>Has<R>\n" +
				"  A\n" +
				"\n",
		},
		{
			"end line next",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 3}, End: engine.Cursor{Line: 4}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(3): message1\n" +
				"\n" +
				"  File\n" +
				"  <G>Has<R>\n" +
				"  <G>A<R>\n" +
				"  Little\n" +
				"\n",
		},
		{
			"end line 3",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 3}, End: engine.Cursor{Line: 5}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(3): message1\n" +
				"\n" +
				"  File\n" +
				"  <G>Has<R>\n" +
				"  <G>A<R>\n" +
				"  <G>Little<R>\n" +
				"  Bit\n" +
				"\n",
		},
		{
			"span 2 lines",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 1, Col: 2}, End: engine.Cursor{Line: 2, Col: 3}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(1): message1\n" +
				"\n" +
				"  T<G>his<R>\n" +
				"  <G>Fi<R>le\n" +
				"  Has\n" +
				"\n",
		},
		{
			"span end col overflow",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 4, Col: 1}, End: engine.Cursor{Line: 5, Col: 11}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(4): message1\n" +
				"\n" +
				"  Has\n" +
				"  <G>A<R>\n" +
				"  <G>Little<R>\n" +
				"  Bit\n" +
				"\n",
		},
		{
			"span intra-line 1 char",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 5, Col: 2}, End: engine.Cursor{Line: 5, Col: 3}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(5): message1\n" +
				"\n" +
				"  A\n" +
				"  L<G>i<R>ttle\n" +
				"  Bit\n" +
				"\n",
		},
		{
			"span intra-line 2 chars",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 5, Col: 2}, End: engine.Cursor{Line: 5, Col: 4}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(5): message1\n" +
				"\n" +
				"  A\n" +
				"  L<G>it<R>tle\n" +
				"  Bit\n" +
				"\n",
		},
		{
			"span intra-line EOL",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 5, Col: 1}, End: engine.Cursor{Line: 5, Col: 7}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(5): message1\n" +
				"\n" +
				"  A\n" +
				"  <G>Little<R>\n" +
				"  Bit\n" +
				"\n",
		},
		{
			"span intra-line EOL overflow",
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 5, Col: 1}, End: engine.Cursor{Line: 5, Col: 11}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(5): message1\n" +
				"\n" +
				"  A\n" +
				"  <G>Little<R>\n" +
				"  Bit\n" +
				"\n",
		},
	}
	for i, l := range data {
		l := l
		t.Run(fmt.Sprintf("%d-%s", i, l.name), func(t *testing.T) {
			t.Parallel()
			buf := bytes.Buffer{}
			// Strip the ANSI codes for now, otherwise it makes the test fairly messy.
			// Note that many of the ANSI code are hacked out in ansi_test.go.
			r := interactive{out: colorable.NewNonColorable(&buf)}
			ctx := context.Background()
			if err := r.EmitFinding(ctx, "mycheck", l.l, "message1", "testdata", l.filepath, l.span, nil); err != nil {
				t.Fatal(err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(l.want, buf.String()); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInteractive(t *testing.T) {
	t.Parallel()
	buf := bytes.Buffer{}
	// Strip the ANSI codes for now, otherwise it makes the test fairly messy.
	// Note that many of the ANSI code are hacked out in ansi_test.go.
	r := interactive{out: colorable.NewNonColorable(&buf)}
	ctx := context.Background()
	if err := r.EmitArtifact(ctx, "mycheck", "", "testdata/file.txt", []byte("content")); err == nil {
		t.Fatal("expected failure")
	}
	start := time.Now()
	r.CheckCompleted(ctx, "mycheck", start, time.Millisecond, engine.Notice, nil)
	r.CheckCompleted(ctx, "badcheck", start, time.Millisecond, engine.Notice, errors.New("bad"))
	r.Print(ctx, "", "src.star", 12, "debugmsg")
	r.Print(ctx, "mycheck", "src.star", 12, "debugmsg")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := "<R>- <G>mycheck<R> (success in 1ms)\n" +
		"<R>- <Re>badcheck<R> (error in 1ms): bad\n" +
		"<R>[src.star:12<R>] <B>debugmsg<R>\n" +
		"<R>- <Y>mycheck <R>[src.star:12<R>] <B>debugmsg<R>\n"

	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestSARIF(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := SarifReport{Out: &buf}
	ctx := context.Background()
	root := t.TempDir()

	if err := r.EmitFinding(
		ctx,
		"check1",
		engine.Error,
		"Found an issue",
		root,
		"foo/bar.c",
		engine.Span{},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	if err := r.EmitFinding(
		ctx,
		"check1",
		engine.Warning,
		"Found another issue",
		root,
		"foo/baz.c",
		engine.Span{
			Start: engine.Cursor{Line: 5, Col: 4},
			End:   engine.Cursor{Line: 6, Col: 2},
		},
		[]string{"blah", "meh"},
	); err != nil {
		t.Fatal(err)
	}

	if err := r.EmitFinding(
		ctx,
		"check2",
		engine.Notice,
		"Notice from check2",
		root,
		"path/to/another_file.rs",
		engine.Span{
			Start: engine.Cursor{Line: 2, Col: 3},
		},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	var got sarif.Document
	if err := protojson.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	want := sarif.Document{
		Version: sarif.Version,
		Runs: []*sarif.Run{
			{
				Tool: &sarif.Tool{Driver: &sarif.ToolComponent{Name: "check1"}},
				Results: []*sarif.Result{
					{
						Level: sarif.Error,
						Message: &sarif.Message{
							Text: "Found an issue",
						},
						Locations: []*sarif.Location{
							{
								PhysicalLocation: &sarif.PhysicalLocation{
									ArtifactLocation: &sarif.ArtifactLocation{
										Uri: "foo/bar.c",
									},
									Region: &sarif.Region{},
								},
							},
						},
					},
					{
						Level: sarif.Warning,
						Message: &sarif.Message{
							Text: "Found another issue",
						},
						Locations: []*sarif.Location{
							{
								PhysicalLocation: &sarif.PhysicalLocation{
									ArtifactLocation: &sarif.ArtifactLocation{
										Uri: "foo/baz.c",
									},
									Region: &sarif.Region{
										StartLine:   5,
										StartColumn: 4,
										EndLine:     6,
										EndColumn:   2,
									},
								},
							},
						},
						Fixes: []*sarif.Fix{
							{
								ArtifactChanges: []*sarif.ArtifactChange{
									{
										ArtifactLocation: &sarif.ArtifactLocation{
											Uri: "foo/baz.c",
										},
										Replacements: []*sarif.Replacement{
											{
												DeletedRegion: &sarif.Region{
													StartLine:   5,
													StartColumn: 4,
													EndLine:     6,
													EndColumn:   2,
												},
												InsertedContent: &sarif.ArtifactContent{
													Text: "blah",
												},
											},
										},
									},
								},
							},
							{
								ArtifactChanges: []*sarif.ArtifactChange{
									{
										ArtifactLocation: &sarif.ArtifactLocation{
											Uri: "foo/baz.c",
										},
										Replacements: []*sarif.Replacement{
											{
												DeletedRegion: &sarif.Region{
													StartLine:   5,
													StartColumn: 4,
													EndLine:     6,
													EndColumn:   2,
												},
												InsertedContent: &sarif.ArtifactContent{
													Text: "meh",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Tool: &sarif.Tool{Driver: &sarif.ToolComponent{Name: "check2"}},
				Results: []*sarif.Result{
					{Level: sarif.Note,
						Message: &sarif.Message{Text: "Notice from check2"},
						Locations: []*sarif.Location{
							{
								PhysicalLocation: &sarif.PhysicalLocation{
									ArtifactLocation: &sarif.ArtifactLocation{
										Uri: "path/to/another_file.rs",
									},
									Region: &sarif.Region{
										StartLine:   2,
										StartColumn: 3,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if diff := cmp.Diff(&want, &got, protocmp.Transform()); diff != "" {
		t.Errorf("SARIF diff (-want +got):\n%s", diff)
	}
}

func init() {
	// Mutate the running environment to make the test deterministic.
	os.Unsetenv("LUCI_CONTEXT")
	os.Unsetenv("GITHUB_RUN_ID")
	os.Unsetenv("VSCODE_GIT_IPC_HANDLE")
	os.Setenv("TERM", "dumb")
}
