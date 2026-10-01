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
	"errors"
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
	results []testResultRecord
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
		"def test_assertions():\n"+
		"    asserts.eq(1, 1)\n"+
		"    asserts.ne(1, 2)\n"+
		"    asserts.true(True)\n"+
		"    asserts.false(False)\n"+
		"    asserts.contains([1, 2, 3], 2)\n"+
		"    asserts.contains(('a', 'b'), 'b')\n"+
		"    asserts.contains({'k': 'v'}, 'k')\n"+
		"    asserts.contains('hello world', 'world')\n"+
		"    asserts.fails(lambda: fail('boom error'), 'boom.*')\n")

	rep := &capturingTestReporter{}
	err := RunTests(t.Context(), &Options{
		Dir:          root,
		TestReporter: rep,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v (results: %+v)", err, rep.results)
	}
	want := []testResultRecord{
		{name: "test_assertions"},
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

func TestRunTests_Failures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name: "asserts.eq single line",
			content: "" +
				"def test_fail():\n" +
				"    asserts.eq('foo', 'bar')\n",
			wantErr: "asserts.eq: assertion failed: got \"foo\", want \"bar\"",
		},
		{
			name: "asserts.eq multi line diff",
			content: "" +
				"def test_fail():\n" +
				"    asserts.eq([1, 2], [1, 3])\n",
			wantErr: "asserts.eq: assertion failed: values are not equal:\n--- expected\n+++ actual\n@@ -1,5 +1,5 @@\n [\n   1,\n-  3,\n+  2,\n ]\n",
		},
		{
			name: "asserts.eq with msg",
			content: "" +
				"def test_fail():\n" +
				"    asserts.eq('foo', 'bar', 'wrong name')\n",
			wantErr: "asserts.eq: assertion failed: wrong name: got \"foo\", want \"bar\"",
		},
		{
			name: "asserts.ne failure",
			content: "" +
				"def test_fail():\n" +
				"    asserts.ne(42, 42)\n",
			wantErr: "asserts.ne: assertion failed: expected values to differ, but both were 42",
		},
		{
			name: "asserts.true failure",
			content: "" +
				"def test_fail():\n" +
				"    asserts.true(False, 'custom msg')\n",
			wantErr: "asserts.true: assertion failed: custom msg",
		},
		{
			name: "asserts.false failure",
			content: "" +
				"def test_fail():\n" +
				"    asserts.false(1)\n",
			wantErr: "asserts.false: assertion failed: expected falsy value, got 1",
		},
		{
			name: "asserts.contains failure",
			content: "" +
				"def test_fail():\n" +
				"    asserts.contains([1, 2], 3)\n",
			wantErr: "asserts.contains: assertion failed: [1, 2] does not contain 3",
		},
		{
			name: "asserts.contains with msg",
			content: "" +
				"def test_fail():\n" +
				"    asserts.contains({'a': 1}, 'b', msg = 'missing key')\n",
			wantErr: "asserts.contains: assertion failed: missing key: {\"a\": 1} does not contain key \"b\"",
		},
		{
			name: "asserts.fails did not fail",
			content: "" +
				"def test_fail():\n" +
				"    asserts.fails(lambda: None)\n",
			wantErr: "asserts.fails: expected function <function lambda> to fail, but it succeeded",
		},
		{
			name: "asserts.fails wrong pattern",
			content: "" +
				"def test_fail():\n" +
				"    asserts.fails(lambda: fail('actual error'), 'expected.*')\n",
			wantErr: "asserts.fails: expected error matching \"expected.*\", got \"fail: actual error\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "fail_test.star", tc.content)
			rep := &capturingTestReporter{}
			err := RunTests(t.Context(), &Options{
				Dir:          root,
				TestReporter: rep,
			})
			if !errors.Is(err, ErrCheckFailed) {
				t.Fatalf("Expected ErrCheckFailed, got %v", err)
			}
			if len(rep.results) != 1 {
				t.Fatalf("Expected 1 test result, got %d", len(rep.results))
			}
			if !strings.Contains(rep.results[0].err, tc.wantErr) {
				t.Fatalf("Expected error containing %q, got %q", tc.wantErr, rep.results[0].err)
			}
		})
	}
}

func TestTestOnlyModulesUnavailableOutsideTestFiles(t *testing.T) {
	t.Parallel()
	for _, module := range []string{"asserts"} {
		t.Run(module+" in shac check", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "shac.star", "print("+module+")\n")
			err := Run(t.Context(), &Options{
				Dir:    root,
				Report: &testNoopReport{},
			})
			want := "undefined: " + module
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Expected error containing %q, got %v", want, err)
			}
		})

		t.Run(module+" in non-test file under shac test", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "helper.star", "x = "+module+"\n")
			writeFile(t, root, "a_test.star", ""+
				"load('//helper.star', 'x')\n"+
				"def test_a():\n"+
				"    pass\n")
			err := RunTests(t.Context(), &Options{
				Dir:          root,
				TestReporter: &capturingTestReporter{},
			})
			want := "undefined: " + module
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Expected error containing %q, got %v", want, err)
			}
		})
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

type testNoopReport struct{}

func (testNoopReport) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string, props map[string]string) error {
	return nil
}

func (testNoopReport) EmitCommitMessageFinding(ctx context.Context, check string, level Level, message string, commitHash string, commitMessage string, s Span, props map[string]string) error {
	return nil
}

func (testNoopReport) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	return nil
}

func (testNoopReport) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level Level, err error) {
}

func (testNoopReport) Print(ctx context.Context, check, file string, line int, message string) {
}
