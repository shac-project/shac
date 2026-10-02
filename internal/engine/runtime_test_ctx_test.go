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
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
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

	writeFile(t, root, "checks.star", ""+
		"def _private_helper(x):\n"+
		"    return x + 1\n"+
		"\n"+
		"def _my_check(ctx, prefix = 'ERR'):\n"+
		"    v = ctx.vars.get('custom_var')\n"+
		"    for path, meta in ctx.scm.affected_files().items():\n"+
		"        content = str(ctx.io.read_file(path))\n"+
		"        for num, line in meta.new_lines():\n"+
		"            if 'BAD' in line:\n"+
		"                ctx.emit.finding(\n"+
		"                    level = 'error',\n"+
		"                    message = prefix + ':' + v + ':' + meta.action,\n"+
		"                    filepath = path,\n"+
		"                    line = num,\n"+
		"                    col = 1,\n"+
		"                    end_line = num,\n"+
		"                    end_col = len(line) + 1,\n"+
		"                    replacements = [line.replace('BAD', 'GOOD')],\n"+
		"                    properties = {'category': 'lint'},\n"+
		"                )\n"+
		"    for c in ctx.scm.commits():\n"+
		"        if 'WIP' in c.message:\n"+
		"            ctx.emit.commit_message_finding(\n"+
		"                level = 'warning',\n"+
		"                message = 'No WIP commits',\n"+
		"                commit = c,\n"+
		"                line = 1,\n"+
		"            )\n"+
		"    ctx.emit.artifact(filepath = 'report.txt', content = 'summary')\n"+
		"\n"+
		"my_check = shac.check(_my_check)\n"+
		"\n"+
		"def register_all():\n"+
		"    shac.register_check(my_check)\n")

	writeFile(t, root, "checks_test.star", ""+
		"load('//checks.star', 'my_check', 'register_all')\n"+
		"\n"+
		"def test_assertions():\n"+
		"    asserts.eq(1, 1)\n"+
		"    asserts.ne(1, 2)\n"+
		"    asserts.true(True)\n"+
		"    asserts.false(False)\n"+
		"    asserts.contains([1, 2, 3], 2)\n"+
		"    asserts.contains(('a', 'b'), 'b')\n"+
		"    asserts.contains({'k': 'v'}, 'k')\n"+
		"    asserts.contains('hello world', 'world')\n"+
		"    asserts.fails(lambda: fail('boom error'), 'boom.*')\n"+
		"\n"+
		"def test_check_and_fixes():\n"+
		"    res = testing.run(\n"+
		"        my_check,\n"+
		"        args = {'prefix': 'CUSTOM'},\n"+
		"        vars = {'custom_var': 'overridden'},\n"+
		"        files = {\n"+
		"            'a.txt': 'ok\\nBAD line\\n',\n"+
		"            'unmodified.txt': testing.file(content = 'BAD ignored', affected = False),\n"+
		"            'deleted.txt': testing.file(action = 'D', content = 'BAD deleted'),\n"+
		"        },\n"+
		"        commits = [\n"+
		"            testing.commit(hash = '123456', message = 'WIP: change'),\n"+
		"        ],\n"+
		"    )\n"+
		"    asserts.eq(\n"+
		"        res.findings,\n"+
		"        (\n"+
		"            testing.finding(\n"+
		"                filepath = 'a.txt',\n"+
		"                level = 'error',\n"+
		"                message = 'CUSTOM:overridden:M',\n"+
		"                line = 2,\n"+
		"                col = 1,\n"+
		"                end_line = 2,\n"+
		"                end_col = 9,\n"+
		"                replacements = ['GOOD line'],\n"+
		"                properties = {'category': 'lint'},\n"+
		"            ),\n"+
		"            testing.finding(\n"+
		"                commit_hash = '123456',\n"+
		"                level = 'warning',\n"+
		"                message = 'No WIP commits',\n"+
		"                line = 1,\n"+
		"            ),\n"+
		"        ),\n"+
		"    )\n"+
		"    asserts.eq(res.files['a.txt'], 'ok\\nGOOD line\\n')\n"+
		"    asserts.eq(res.artifacts['report.txt'], 'summary')\n"+
		"\n"+
		"def test_register_function():\n"+
		"    res = testing.run(\n"+
		"        register_all,\n"+
		"        files = {'a.txt': 'BAD\\n'},\n"+
		"    )\n"+
		"    asserts.eq(len(res.findings), 1)\n"+
		"    asserts.eq(res.files['a.txt'], 'GOOD\\n')\n"+
		"\n"+
		"def test_exec_mock_and_write_file():\n"+
		"    def _exec_check(ctx):\n"+
		"        print('from check')\n"+
		"        tmp = ctx.io.tempfile('initial', name = 'input.txt')\n"+
		"        res1 = ctx.os.exec(['my_tool', '--fix', tmp]).wait()\n"+
		"        asserts.eq(res1.stdout, 'root=' + ctx.scm.root)\n"+
		"        updated = str(ctx.io.read_file(tmp))\n"+
		"        ctx.emit.finding(\n"+
		"            level = 'error',\n"+
		"            message = 'fixed',\n"+
		"            filepath = 'f.txt',\n"+
		"            line = 1,\n"+
		"            col = 1,\n"+
		"            end_line = 1,\n"+
		"            end_col = 8,\n"+
		"            replacements = [updated],\n"+
		"        )\n"+
		"\n"+
		"    def _handler(cmd):\n"+
		"        print('from handler')\n"+
		"        testing.write_file(cmd[2], 'updated_by_tool')\n"+
		"        return testing.exec_result(stdout = 'root=' + testing.root)\n"+
		"\n"+
		"    res = testing.run(\n"+
		"        _exec_check,\n"+
		"        files = {'f.txt': 'initial'},\n"+
		"        exec_mocks = [\n"+
		"            testing.exec_mock(\n"+
		"                cmd = ['my_tool', '--fix', testing.any_args],\n"+
		"                handler = _handler,\n"+
		"            ),\n"+
		"        ],\n"+
		"    )\n"+
		"    asserts.eq(res.files['f.txt'], 'updated_by_tool')\n")

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
		{name: "test_check_and_fixes"},
		{name: "test_exec_mock_and_write_file", prints: []string{
			"[//checks_test.star:63] from check",
			"[//checks_test.star:80] from handler",
		}},
		{name: "test_register_function"},
	}
	if diff := cmp.Diff(want, rep.results, cmp.AllowUnexported(testResultRecord{})); diff != "" {
		t.Fatalf("Unexpected test results (-want +got):\n%s", diff)
	}
}

func TestExecMockMatches(t *testing.T) {
	t.Parallel()
	const root = "/tmp/root"
	data := []struct {
		name string
		want []starlark.Value
		cmd  []string
		ok   bool
	}{
		{"exact", []starlark.Value{starlark.String("tool"), starlark.String("-x")}, []string{"tool", "-x"}, true},
		{"extra arg", []starlark.Value{starlark.String("tool")}, []string{"tool", "-x"}, false},
		{"missing arg", []starlark.Value{starlark.String("tool"), starlark.String("-x")}, []string{"tool"}, false},
		{"trailing any matches none", []starlark.Value{starlark.String("tool"), anyArgs}, []string{"tool"}, true},
		{"trailing any matches many", []starlark.Value{starlark.String("tool"), anyArgs}, []string{"tool", "-a", "-b", "f"}, true},
		{"middle any", []starlark.Value{starlark.String("tool"), anyArgs, starlark.String("./...")}, []string{"tool", "-a", "-b", "./..."}, true},
		{"middle any wrong suffix", []starlark.Value{starlark.String("tool"), anyArgs, starlark.String("./...")}, []string{"tool", "-a", "f"}, false},
		{"backtracks past early match", []starlark.Value{starlark.String("tool"), anyArgs, starlark.String("f")}, []string{"tool", "f", "-a", "f"}, true},
		{"multiple anys", []starlark.Value{anyArgs, starlark.String("-x"), anyArgs}, []string{"tool", "a", "-x", "b"}, true},
		{"absolute tool path", []starlark.Value{starlark.String(".tools/gosec"), anyArgs}, []string{root + "/.tools/gosec", "-quiet"}, true},
		{"root placeholder", []starlark.Value{starlark.String("tool"), starlark.String(testingRootPlaceholder + "/a/../b")}, []string{"tool", root + "/b"}, true},
	}
	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			m := &execMockEntry{cmd: starlark.Tuple(d.want)}
			if got := m.matches(d.cmd, root); got != d.ok {
				t.Errorf("matches(%q) = %v, want %v", d.cmd, got, d.ok)
			}
		})
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
			wantErr: "asserts.contains: assertion failed: missing key: {\"a\": 1} does not contain \"b\"",
		},
		{
			name: "asserts.contains type error",
			content: "" +
				"def test_fail():\n" +
				"    asserts.contains('abc', 1)\n",
			wantErr: "asserts.contains: 'in <string>' requires string as left operand, not int",
		},
		{
			name: "asserts.fails invalid regexp",
			content: "" +
				"def test_fail():\n" +
				"    asserts.fails(lambda: fail('boom'), '(')\n",
			wantErr: "asserts.fails: for parameter \"msg\": error parsing regexp: missing closing )",
		},
		{
			name: "asserts.eq self-referential list",
			content: "" +
				"def test_fail():\n" +
				"    a = []\n" +
				"    a.append(a)\n" +
				"    asserts.eq(a, [1])\n",
			wantErr: "asserts.eq: assertion failed",
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
		{
			name: "unused exec_mock",
			content: "" +
				"def _cb(ctx):\n" +
				"    pass\n" +
				"def test_fail():\n" +
				"    testing.run(_cb, exec_mocks = [testing.exec_mock(cmd = ['unused'])])\n",
			wantErr: "unused exec_mock: (\"unused\",)",
		},
		{
			name: "exec_mock negative retcode",
			content: "" +
				"def test_fail():\n" +
				"    testing.exec_mock(cmd = ['foo'], retcode = -1)\n",
			wantErr: "testing.exec_mock: for parameter \"retcode\": got -1, want a non-negative int",
		},
		{
			name: "exec_result negative retcode",
			content: "" +
				"def test_fail():\n" +
				"    testing.exec_result(retcode = -1)\n",
			wantErr: "testing.exec_result: for parameter \"retcode\": got -1, want a non-negative int",
		},
		{
			name: "write_file outside testing.run",
			content: "" +
				"def test_fail():\n" +
				"    testing.write_file('foo.txt', 'x')\n",
			wantErr: "testing.write_file: can only be called during testing.run()",
		},
		{
			name: "out of bounds replacement span",
			content: "" +
				"def _cb(ctx):\n" +
				"    ctx.emit.finding(\n" +
				"        level = 'error',\n" +
				"        message = 'bad span',\n" +
				"        filepath = 'a.txt',\n" +
				"        line = 5,\n" +
				"        end_line = 6,\n" +
				"        replacements = ['fixed'],\n" +
				"    )\n" +
				"def test_fail():\n" +
				"    testing.run(_cb, files = {'a.txt': 'single line\\n'})\n",
			wantErr: "check \"cb\" emitted finding with span (lines 5-6) beyond end of file (1 line)",
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
	for _, module := range []string{"asserts", "testing"} {
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

type backtraceTestReporter struct {
	capturingTestReporter
	backtraces []string
}

func (r *backtraceTestReporter) TestResult(ctx context.Context, name, file string, d time.Duration, err error, prints []string) {
	r.capturingTestReporter.TestResult(ctx, name, file, d, err, prints)
	if bt, ok := errors.AsType[BacktraceableError](err); ok {
		r.mu.Lock()
		r.backtraces = append(r.backtraces, bt.Backtrace())
		r.mu.Unlock()
	}
}

func TestRunTests_CheckFailureBacktrace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		failure string
	}{
		{"fail", "fail('boom')"},
		{"runtime error", "1 // 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "a_test.star", ""+
				"def _helper():\n"+
				"    "+tc.failure+"\n"+
				"def _check(ctx):\n"+
				"    _helper()\n"+
				"def test_a():\n"+
				"    testing.run(_check)\n")
			rep := &backtraceTestReporter{}
			err := RunTests(t.Context(), &Options{Dir: root, TestReporter: rep})
			if !errors.Is(err, ErrCheckFailed) {
				t.Fatalf("Expected ErrCheckFailed, got %v", err)
			}
			if len(rep.backtraces) != 1 {
				t.Fatalf("Expected one backtrace, got %+v", rep.results)
			}
			// The backtrace should go from the test function through
			// testing.run() into the check, down to where it failed.
			for _, want := range []string{"in test_a", "in _check", "in _helper"} {
				if !strings.Contains(rep.backtraces[0], want) {
					t.Errorf("Backtrace missing %q:\n%s", want, rep.backtraces[0])
				}
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
