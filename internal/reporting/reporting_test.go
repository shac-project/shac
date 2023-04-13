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
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mattn/go-colorable"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
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
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message1", "", "", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message2", "", "testdata/file.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message3", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "testdata/file.txt", []byte("content")); err == nil {
		t.Fatal("expected failure")
	}
	start := time.Now()
	r.CheckCompleted(ctx, "mycheck", start, time.Millisecond, engine.Notice, nil)
	r.CheckCompleted(ctx, "badcheck", start, time.Millisecond, engine.Notice, errors.New("bad"))
	r.Print(ctx, "src.star", 12, "debugmsg")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := "[mycheck/notice] message1\n" +
		"[mycheck/notice] testdata/file.txt: message2\n" +
		"[mycheck/notice] testdata/file.txt(10): message3\n" +
		"- mycheck (success in 1ms)\n" +
		"- badcheck (error in 1ms): bad\n" +
		"[src.star:12] debugmsg\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestGitHub(t *testing.T) {
	buf := bytes.Buffer{}
	r := github{out: &buf}
	ctx := context.Background()
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message1", "", "", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message2", "", "testdata/file.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message3", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message4", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10, Col: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message5", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10}, End: engine.Cursor{Line: 12}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message6", "", "testdata/file.txt", engine.Span{Start: engine.Cursor{Line: 10, Col: 1}, End: engine.Cursor{Line: 12, Col: 2}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "testdata/file.txt", []byte("content")); err == nil {
		t.Fatal("expected failure")
	}
	start := time.Now()
	r.CheckCompleted(ctx, "mycheck", start, time.Millisecond, engine.Notice, nil)
	r.CheckCompleted(ctx, "badcheck", start, time.Millisecond, engine.Notice, errors.New("bad"))
	r.Print(ctx, "src.star", 12, "debugmsg")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := "::notice ::title=mycheck::message1\n" +
		"::notice ::file=testdata/file.txttitle=mycheck::message2\n" +
		"::notice ::file=testdata/file.txt,line=10,title=mycheck::message3\n" +
		"::notice ::file=testdata/file.txt,line=10,col=1,title=mycheck::message4\n" +
		"::notice ::file=testdata/file.txt,line=10,endLine=12,title=mycheck::message5\n" +
		"::notice ::file=testdata/file.txt,line=10,col=1,endLine=12,endCol=2,title=mycheck::message6\n" +
		"::debug::[src.star:12] debugmsg\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestInteractive_Annotation(t *testing.T) {
	t.Parallel()
	data := []struct {
		l        engine.Level
		filepath string
		span     engine.Span
		want     string
	}{
		{
			engine.Notice,
			"",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] message1\n",
		},
		{
			engine.Warning,
			"",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<Y>warning<R>] message1\n",
		},
		{
			engine.Error,
			"",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<Re>error<R>] message1\n",
		},
		{
			engine.Notice,
			"file.txt",
			engine.Span{},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt: message1\n",
		},
		{
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
			// Span with columns.
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 1, Col: 2}, End: engine.Cursor{Line: 2, Col: 2}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(1): message1\n" +
				"\n" +
				"  T<G>his<R>\n" +
				"  <G>Fi<R>le\n" +
				"  Has\n" +
				"\n",
		},
		{
			// Intra-line.
			engine.Notice,
			"file.txt",
			engine.Span{Start: engine.Cursor{Line: 5, Col: 2}, End: engine.Cursor{Line: 5, Col: 3}},
			"<R>[<Hc>mycheck<R>/<G>notice<R>] file.txt(5): message1\n" +
				"\n" +
				"  A\n" +
				"  L<G>it<R>tle\n" +
				"  Bit\n" +
				"\n",
		},
	}
	for i, l := range data {
		l := l
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			buf := bytes.Buffer{}
			// Strip the ANSI codes for now, otherwise it makes the test fairly messy.
			// Note that many of the ANSI code are hacked out in ansi_test.go.
			r := interactive{out: colorable.NewNonColorable(&buf)}
			ctx := context.Background()
			if err := r.EmitAnnotation(ctx, "mycheck", l.l, "message1", "testdata", l.filepath, l.span, nil); err != nil {
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
	r.Print(ctx, "src.star", 12, "debugmsg")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := "<R>- <G>mycheck<R> (success in 1ms)\n" +
		"<R>- <Re>badcheck<R> (error in 1ms): bad\n" +
		"<R>[src.star:12<R>] <B>debugmsg<R>\n"

	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func init() {
	// Mutate the running environment to make the test deterministic.
	os.Unsetenv("LUCI_CONTEXT")
	os.Unsetenv("GITHUB_RUN_ID")
	os.Unsetenv("VSCODE_GIT_IPC_HANDLE")
	os.Setenv("TERM", "dumb")
}
