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
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func BenchmarkPrint_Raw(b *testing.B) {
	root := b.TempDir()
	copyFile(b, root, "testdata/bench/print.star")
	want := "[//print.star:16] running\n"
	benchStarlarkPrint(b, root, "print.star", true, want)
}

func BenchmarkPrint_Git(b *testing.B) {
	want := "[//print.star:16] running\n"
	benchStarlarkPrint(b, "testdata/bench", "print.star", true, want)
}

func BenchmarkPrint100_Raw(b *testing.B) {
	root := b.TempDir()
	copyFile(b, root, "testdata/bench/print100.star")
	want := strings.Repeat("[//print100.star:16] running\n", 100)
	benchStarlarkPrint(b, root, "print100.star", true, want)
}

func BenchmarkPrint100_Git(b *testing.B) {
	want := strings.Repeat("[//print100.star:16] running\n", 100)
	benchStarlarkPrint(b, "testdata/bench", "print100.star", true, want)
}

func BenchmarkCtxEmitAnnotation(b *testing.B) {
	// Use ctx-emit-annotation-warning.star since it only emit once, which makes
	// understanding memory allocation easier.
	root := b.TempDir()
	copyFile(b, root, "testdata/bench/ctx-emit-annotation.star")
	copyFile(b, root, "testdata/bench/file.txt")
	r := reportEmitNoPrint{reportNoPrint: reportNoPrint{t: b}}
	o := Options{Report: &r, Root: root, main: "ctx-emit-annotation.star"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Run(context.Background(), &o); err != nil {
			b.Fatal(err)
		}
		r.annotations = r.annotations[:0]
		r.artifacts = r.artifacts[:0]
	}
}

func BenchmarkCtxEmitArtifact(b *testing.B) {
	root := b.TempDir()
	copyFile(b, root, "testdata/bench/ctx-emit-artifact.star")
	copyFile(b, root, "testdata/bench/file.txt")
	r := reportEmitNoPrint{reportNoPrint: reportNoPrint{t: b}}
	o := Options{Report: &r, Root: root, main: "ctx-emit-artifact.star"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Run(context.Background(), &o); err != nil {
			b.Fatal(err)
		}
		r.annotations = r.annotations[:0]
		r.artifacts = r.artifacts[:0]
	}
}

func BenchmarkCtxOsExec(b *testing.B) {
	root := b.TempDir()
	copyFile(b, root, "testdata/bench/ctx-os-exec.star")
	copyFile(b, root, "testdata/bench/stdio.bat")
	copyFile(b, root, "testdata/bench/stdio.sh")
	want := "[//ctx-os-exec.star:21] retcode: 0\n" +
		"[//ctx-os-exec.star:22] stdout: hello from stdout\n" +
		"[//ctx-os-exec.star:23] stderr: hello from stderr\n"
	benchStarlarkPrint(b, root, "ctx-os-exec.star", true, want)
}

func BenchmarkCtxOsExec100(b *testing.B) {
	root := b.TempDir()
	copyFile(b, root, "testdata/bench/ctx-os-exec100.star")
	copyFile(b, root, "testdata/bench/stdio.bat")
	copyFile(b, root, "testdata/bench/stdio.sh")
	want := "[//ctx-os-exec100.star:24] retcode: 0\n" +
		"stdout: hello from stdout\n" +
		"stderr: hello from stderr\n"
	want = strings.Repeat(want, 100)
	benchStarlarkPrint(b, root, "ctx-os-exec100.star", true, want)
}

func BenchmarkCtxScmNewLines_Git(b *testing.B) {
	root := makeGit(b)
	copyBenchSCM(b, root)
	runGit(b, root, "add", "ctx-scm-*.star")
	want := "[//ctx-scm-affected_files-new_lines.star:23] ctx-scm-affected_files-new_lines.star\n" +
		"1: # Copyright 2023 The Shac Authors\n"
	benchStarlarkPrint(b, root, "ctx-scm-affected_files-new_lines.star", false, want)
}

func BenchmarkCtxScmNewLines100_Git(b *testing.B) {
	root := makeGit(b)
	copyBenchSCM(b, root)
	runGit(b, root, "add", "ctx-scm-*.star")
	want := "[//ctx-scm-affected_files-new_lines100.star:23] ctx-scm-affected_files-new_lines.star\n" +
		"1: # Copyright 2023 The Shac Authors\n"
	want = strings.Repeat(want, 100)
	benchStarlarkPrint(b, root, "ctx-scm-affected_files-new_lines100.star", false, want)
}

func BenchmarkCtxScmNewLines_Raw(b *testing.B) {
	root := b.TempDir()
	writeFile(b, root, "a.txt", "First file")
	copyBenchSCM(b, root)
	want := "[//ctx-scm-affected_files-new_lines.star:23] a.txt\n" +
		"1: First file\n"
	benchStarlarkPrint(b, root, "ctx-scm-affected_files-new_lines.star", false, want)
}

// TODO(maruel): Add large synthetic benchmark.

// benchStarlarkPrint benchmarks a starlark file that calls print().
func benchStarlarkPrint(b *testing.B, root, name string, all bool, want string) {
	r := reportPrint{reportNoPrint: reportNoPrint{t: b}}
	o := Options{Report: &r, Root: root, AllFiles: all, main: name}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Run(context.Background(), &o); err != nil {
			var err2 BacktraceableError
			if errors.As(err, &err2) {
				b.Fatal(err2.Backtrace())
			}
			b.Fatal(err)
		}
		if i == 0 {
			if got := r.b.String(); got != want {
				b.Helper()
				b.Fatalf("mismatch (-want +got):\n%s", cmp.Diff(want, got))
			}
		}
		// We need to reset the buffer to reuse it, but Reset() simply ditch the
		// buffer. Use Grow() right after so there's only one memory allocation (as
		// overhead) per test case.
		l := r.b.Len()
		r.b.Reset()
		r.b.Grow(l)
	}
}

func copyBenchSCM(t testing.TB, dst string) {
	m, err := filepath.Glob(filepath.Join("testdata", "bench", "ctx-scm-*.star"))
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range m {
		copyFile(t, dst, src)
	}
}
