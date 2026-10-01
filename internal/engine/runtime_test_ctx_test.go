// Copyright 2026 The Shac Authors
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
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type testResultRecord struct {
	name   string
	err    string
	prints []string
}

type capturingTestReporter struct {
	mu      sync.Mutex
	prints  []string
	results []testResultRecord
}

func (r *capturingTestReporter) Print(ctx context.Context, file string, line int, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prints = append(r.prints, fmt.Sprintf("[%s:%d] %s", file, line, msg))
}

func (r *capturingTestReporter) TestResult(ctx context.Context, name, file string, d time.Duration, err error, prints []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	r.results = append(r.results, testResultRecord{
		name:   name,
		err:    errStr,
		prints: prints,
	})
}

func TestRunTests_Success(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	writeFile(t, root, "shac.textproto", ""+
		"vars: [\n"+
		"  { name: \"custom_var\" default: \"default_val\" }\n"+
		"]\n"+
		"allowed_findings_properties: {\n"+
		"  properties: [\n"+
		"    { name: \"category\" }\n"+
		"  ]\n"+
		"}\n"+
		"ignore_tests: [\n"+
		"  \"/ignored/\"\n"+
		"]\n")

	writeFile(t, root, "ignored/bad_test.star", "fail('should be ignored')\n")

	writeFile(t, root, "checks_test.star", ""+
		"def test_pass():\n"+
		"    pass\n")

	rep := &capturingTestReporter{}
	err := RunTests(t.Context(), &Options{
		Dir:          root,
		TestReporter: rep,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v (results: %+v)", err, rep.results)
	}
	want := []testResultRecord{
		{name: "test_pass"},
	}
	if diff := cmp.Diff(want, rep.results, cmp.AllowUnexported(testResultRecord{})); diff != "" {
		t.Fatalf("Unexpected test results (-want +got):\n%s", diff)
	}
}

func TestRunTests_Filtering(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a_test.star", ""+
		"def test_one():\n"+
		"    pass\n"+
		"def test_two():\n"+
		"    pass\n")
	writeFile(t, root, "b_test.star", ""+
		"def test_two():\n"+
		"    pass\n"+
		"def test_three():\n"+
		"    pass\n")

	rep := &capturingTestReporter{}
	err := RunTests(t.Context(), &Options{
		Dir:          root,
		TestReporter: rep,
		Filter: CheckFilter{
			AllowList: []string{"a_test.star:test_two", "test_three"},
			DenyList:  []string{"b_test.star:test_two"},
		},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	want := []testResultRecord{
		{name: "a_test.star:test_two"},
		{name: "b_test.star:test_three"},
	}
	if diff := cmp.Diff(want, rep.results, cmp.AllowUnexported(testResultRecord{})); diff != "" {
		t.Fatalf("Unexpected test results (-want +got):\n%s", diff)
	}
}

// Not parallel because it changes the working directory.
func TestRunTests_RelativeFileArgs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "sub/one_test.star", "def test_one(): pass\n")
	t.Chdir(filepath.Join(root, "sub"))

	rep := &capturingTestReporter{}
	err := RunTests(t.Context(), &Options{
		Dir:          root,
		Files:        []string{"one_test.star"},
		TestReporter: rep,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	want := []testResultRecord{
		{name: "test_one"},
	}
	if diff := cmp.Diff(want, rep.results, cmp.AllowUnexported(testResultRecord{})); diff != "" {
		t.Fatalf("Unexpected test results (-want +got):\n%s", diff)
	}
}

func TestRunTests_InvalidFilter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "a_test.star", ""+
		"def test_alice():\n"+
		"    pass\n"+
		"def test_bob():\n"+
		"    pass\n")

	data := []struct {
		name    string
		filter  CheckFilter
		wantErr string
	}{
		{
			name:    "allowed and denied",
			filter:  CheckFilter{AllowList: []string{"test_alice"}, DenyList: []string{"test_alice"}},
			wantErr: "tests cannot be both allowed and denied: test_alice",
		},
		{
			name:    "nonexistent allowed test alongside a real one",
			filter:  CheckFilter{AllowList: []string{"test_alice", "test_typo"}},
			wantErr: "test does not exist: test_typo",
		},
		{
			name:    "nonexistent tests in both lists",
			filter:  CheckFilter{AllowList: []string{"test_alice", "a_test.star:test_nope"}, DenyList: []string{"test_typo"}},
			wantErr: "tests do not exist: a_test.star:test_nope, test_typo",
		},
	}
	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			rep := &capturingTestReporter{}
			err := RunTests(t.Context(), &Options{
				Dir:          root,
				TestReporter: rep,
				Filter:       d.filter,
			})
			if err == nil || err.Error() != d.wantErr {
				t.Fatalf("Expected error %q, got %v", d.wantErr, err)
			}
			if len(rep.results) != 0 {
				t.Fatalf("Expected no tests to run, got %+v", rep.results)
			}
		})
	}
}

func TestRunTests_LoadTimePrints(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "helper.star", "print('loading helper')\nx = 1\n")
	writeFile(t, root, "a_test.star", ""+
		"load('helper.star', 'x')\n"+
		"print('loading test')\n"+
		"def test_a():\n"+
		"    pass\n")

	rep := &capturingTestReporter{}
	err := RunTests(t.Context(), &Options{
		Dir:          root,
		TestReporter: rep,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	want := []string{
		"[//helper.star:1] loading helper",
		"[//a_test.star:2] loading test",
	}
	if diff := cmp.Diff(want, rep.prints); diff != "" {
		t.Fatalf("Unexpected prints (-want +got):\n%s", diff)
	}
}

func TestRunTests_RegisterCheckInTestFile(t *testing.T) {
	t.Parallel()
	const wantErr = "shac.register_check: can't register checks directly in a test file"

	t.Run("top level", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "a_test.star", ""+
			"shac.register_check(shac.check(lambda ctx: None, name = 'alice'))\n"+
			"def test_a():\n"+
			"    pass\n")
		err := RunTests(t.Context(), &Options{
			Dir:          root,
			TestReporter: &capturingTestReporter{},
		})
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("Expected error containing %q, got %v", wantErr, err)
		}
	})

	t.Run("in test function", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "a_test.star", ""+
			"def test_a():\n"+
			"    shac.register_check(shac.check(lambda ctx: None, name = 'alice'))\n")
		rep := &capturingTestReporter{}
		err := RunTests(t.Context(), &Options{
			Dir:          root,
			TestReporter: rep,
		})
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if len(rep.results) != 1 || !strings.Contains(rep.results[0].err, wantErr) {
			t.Fatalf("Expected test_a to fail with %q, got %+v", wantErr, rep.results)
		}
	})

}

func TestRunTests_ExplicitFileArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "one_test.star", "def test_one(): pass\n")
	writeFile(t, root, "two_test.star", "def test_two(): fail('should not run')\n")

	rep := &capturingTestReporter{}
	err := RunTests(t.Context(), &Options{
		Dir:          root,
		Files:        []string{filepath.Join(root, "one_test.star")},
		TestReporter: rep,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	want := []testResultRecord{
		{name: "test_one"},
	}
	if diff := cmp.Diff(want, rep.results, cmp.AllowUnexported(testResultRecord{})); diff != "" {
		t.Fatalf("Unexpected test results (-want +got):\n%s", diff)
	}
}
