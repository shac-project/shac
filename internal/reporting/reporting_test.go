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
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message2", "", "foo.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message3", "", "foo.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "file.txt", []byte("content")); err == nil {
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
		"[mycheck/notice] foo.txt: message2\n" +
		"[mycheck/notice] foo.txt(10): message3\n" +
		"- mycheck (Success in 1ms)\n" +
		"- badcheck (Success in 1ms): bad\n" +
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
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message2", "", "foo.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message3", "", "foo.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message4", "", "foo.txt", engine.Span{Start: engine.Cursor{Line: 10, Col: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message5", "", "foo.txt", engine.Span{Start: engine.Cursor{Line: 10}, End: engine.Cursor{Line: 12}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message6", "", "foo.txt", engine.Span{Start: engine.Cursor{Line: 10, Col: 1}, End: engine.Cursor{Line: 12, Col: 2}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "file.txt", []byte("content")); err == nil {
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
		"::notice ::file=foo.txttitle=mycheck::message2\n" +
		"::notice ::file=foo.txt,line=10,title=mycheck::message3\n" +
		"::notice ::file=foo.txt,line=10,col=1,title=mycheck::message4\n" +
		"::notice ::file=foo.txt,line=10,endLine=12,title=mycheck::message5\n" +
		"::notice ::file=foo.txt,line=10,col=1,endLine=12,endCol=2,title=mycheck::message6\n" +
		"::debug::[src.star:12] debugmsg\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestInteractive(t *testing.T) {
	buf := bytes.Buffer{}
	// Strip the ANSI codes for now, otherwise it makes the test fairly messy.
	r := interactive{out: colorable.NewNonColorable(&buf)}
	ctx := context.Background()
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message1", "", "", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message2", "", "foo.txt", engine.Span{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitAnnotation(ctx, "mycheck", engine.Notice, "message3", "", "foo.txt", engine.Span{Start: engine.Cursor{Line: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.EmitArtifact(ctx, "mycheck", "", "file.txt", []byte("content")); err == nil {
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
		"[mycheck/notice] foo.txt: message2\n" +
		"[mycheck/notice] foo.txt(10): message3\n" +
		"- mycheck (Success in 1ms)\n" +
		"- badcheck (Success in 1ms): bad\n" +
		"[src.star:12] debugmsg\n"
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
