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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/pmezard/go-difflib/difflib"
	"go.fuchsia.dev/shac-project/shac/internal/sandbox"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// getAsserts returns the predeclared asserts module.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func getAsserts() starlark.StringDict {
	return starlark.StringDict{
		"contains": newBuiltinNone("asserts.contains", assertContains),
		"eq":       newBuiltinNone("asserts.eq", assertEq),
		"fails":    starlark.NewBuiltin("asserts.fails", assertFails),
		"false":    newBuiltinNone("asserts.false", assertFalse),
		"ne":       newBuiltinNone("asserts.ne", assertNe),
		"true":     newBuiltinNone("asserts.true", assertTrue),
	}
}

// getTesting returns the predeclared testing module.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func getTesting() starlark.StringDict {
	return starlark.StringDict{
		"commit":  newBuiltin("testing.commit", testingCommit),
		"file":    newBuiltin("testing.file", testingFile),
		"finding": newBuiltin("testing.finding", testingFinding),
		"run":     starlark.NewBuiltin("testing.run", withCheckBacktrace(testingRun)),
	}
}

// assertionFailure formats an assertion error, prefixing detail with the
// user-provided msg (if any) so the failure explains what was being checked
// without losing the values that caused it.
func assertionFailure(msg starlark.String, format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	if msg != "" {
		return fmt.Errorf("assertion failed: %s: %s", string(msg), detail)
	}
	return fmt.Errorf("assertion failed: %s", detail)
}

func assertEq(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var actual, expected starlark.Value
	var msg starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"actual", &actual,
		"expected", &expected,
		"msg?", &msg,
	); err != nil {
		return err
	}
	eq, err := starlark.Equal(actual, expected)
	if err != nil {
		return err
	}
	if eq {
		return nil
	}
	actualPretty := prettyStarlarkValue(actual, 0)
	expectedPretty := prettyStarlarkValue(expected, 0)
	if strings.Contains(actualPretty, "\n") || strings.Contains(expectedPretty, "\n") {
		diff, diffErr := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(expectedPretty + "\n"),
			B:        difflib.SplitLines(actualPretty + "\n"),
			FromFile: "expected",
			ToFile:   "actual",
			Context:  3,
		})
		if diffErr == nil && diff != "" {
			return assertionFailure(msg, "values are not equal:\n%s", strings.TrimSuffix(diff, "\n"))
		}
	}
	return assertionFailure(msg, "got %s, want %s", actual.String(), expected.String())
}

func assertNe(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var actual, expected starlark.Value
	var msg starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"actual", &actual,
		"expected", &expected,
		"msg?", &msg,
	); err != nil {
		return err
	}
	eq, err := starlark.Equal(actual, expected)
	if err != nil {
		return err
	}
	if eq {
		return assertionFailure(msg, "expected values to differ, but both were %s", actual.String())
	}
	return nil
}

func assertTrue(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var cond starlark.Value
	var msg starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cond", &cond,
		"msg?", &msg,
	); err != nil {
		return err
	}
	if !cond.Truth() {
		if msg != "" {
			return fmt.Errorf("assertion failed: %s", string(msg))
		}
		return fmt.Errorf("assertion failed: expected truthy value, got %s", cond.String())
	}
	return nil
}

func assertFalse(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var cond starlark.Value
	var msg starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cond", &cond,
		"msg?", &msg,
	); err != nil {
		return err
	}
	if cond.Truth() {
		if msg != "" {
			return fmt.Errorf("assertion failed: %s", string(msg))
		}
		return fmt.Errorf("assertion failed: expected falsy value, got %s", cond.String())
	}
	return nil
}

func assertContains(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var container, item starlark.Value
	var msg starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"container", &container,
		"item", &item,
		"msg?", &msg,
	); err != nil {
		return err
	}
	// Delegating to the "in" operator keeps asserts.contains(c, x) exactly
	// equivalent to asserts.true(x in c), just with a more useful message.
	found, err := starlark.Binary(syntax.IN, item, container)
	if err != nil {
		return err
	}
	if !found.Truth() {
		return assertionFailure(msg, "%s does not contain %s", container.String(), item.String())
	}
	return nil
}

func assertFails(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var target starlark.Callable
	var msg starlark.String
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"fn", &target,
		"msg?", &msg,
	); err != nil {
		return nil, err
	}
	ctx := getContext(th)
	s := ctxShacState(ctx)
	c := ctxCheck(ctx)
	prevStateFail := s.failErr
	var prevCheckFail *failure
	if c != nil {
		prevCheckFail = c.failErr
	}
	_, callErr := starlark.Call(th, target, nil, nil)
	// Restore failErr on the enclosing state/check so an expected fail() call
	// inside target does not mark the enclosing test as having failed.
	s.failErr = prevStateFail
	if c != nil {
		c.failErr = prevCheckFail
	}
	if callErr == nil {
		return nil, fmt.Errorf("%s: expected function %s to fail, but it succeeded", fn.Name(), target.String())
	}
	if msg != "" {
		pattern := string(msg)
		errText := callErr.Error()
		if !strings.Contains(errText, pattern) {
			matched, reErr := regexp.MatchString(pattern, errText)
			if reErr != nil {
				return nil, fmt.Errorf("%s: for parameter \"msg\": %w", fn.Name(), reErr)
			}
			if !matched {
				return nil, fmt.Errorf("%s: expected error matching %q, got %q", fn.Name(), pattern, errText)
			}
		}
	}
	return starlark.None, nil
}

// maxPrettyDepth bounds prettyStarlarkValue's recursion so self-referential
// containers (e.g. a list that contains itself) can't overflow the stack.
// Beyond this depth, values fall back to String(), which handles cycles.
const maxPrettyDepth = 32

func prettyStarlarkValue(v starlark.Value, indent int) string {
	if indent >= maxPrettyDepth {
		return v.String()
	}
	pad := strings.Repeat("  ", indent)
	innerPad := strings.Repeat("  ", indent+1)
	switch val := v.(type) {
	case starlark.String:
		if indent == 0 && strings.Contains(string(val), "\n") {
			return string(val)
		}
		return val.String()
	case *starlarkstruct.Struct:
		names := val.AttrNames()
		if len(names) == 0 {
			return fmt.Sprintf("%s()", val.Constructor())
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s(\n", val.Constructor())
		for _, k := range names {
			attr, err := val.Attr(k)
			if err != nil {
				continue
			}
			fmt.Fprintf(&b, "%s%s = %s,\n", innerPad, k, prettyStarlarkValue(attr, indent+1))
		}
		fmt.Fprintf(&b, "%s)", pad)
		return b.String()
	case starlark.Tuple:
		if len(val) == 0 {
			return "()"
		}
		var b strings.Builder
		b.WriteString("(\n")
		for _, item := range val {
			fmt.Fprintf(&b, "%s%s,\n", innerPad, prettyStarlarkValue(item, indent+1))
		}
		fmt.Fprintf(&b, "%s)", pad)
		return b.String()
	case *starlark.List:
		if val.Len() == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		for i := range val.Len() {
			fmt.Fprintf(&b, "%s%s,\n", innerPad, prettyStarlarkValue(val.Index(i), indent+1))
		}
		fmt.Fprintf(&b, "%s]", pad)
		return b.String()
	case *starlark.Dict:
		items := val.Items()
		if len(items) == 0 {
			return "{}"
		}
		var b strings.Builder
		b.WriteString("{\n")
		for _, kv := range items {
			fmt.Fprintf(&b, "%s%s: %s,\n", innerPad, kv[0].String(), prettyStarlarkValue(kv[1], indent+1))
		}
		fmt.Fprintf(&b, "%s}", pad)
		return b.String()
	default:
		return v.String()
	}
}

func testingFile(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argcontent starlark.String
	var argaction starlark.String = "M"
	var argnewLines starlark.Value = starlark.None
	var argaffected starlark.Bool = true
	if err := starlark.UnpackArgs(name, args, kwargs,
		"content?", &argcontent,
		"action?", &argaction,
		"new_lines??", &argnewLines,
		"affected?", &argaffected,
	); err != nil {
		return nil, err
	}
	if argaction == "" {
		return nil, errors.New("for parameter \"action\": must not be empty")
	}
	parsedLines, err := parseCustomNewLines(argnewLines)
	if err != nil {
		return nil, err
	}
	res := toValue("file_spec", starlark.StringDict{
		"action":    argaction,
		"affected":  argaffected,
		"content":   argcontent,
		"new_lines": parsedLines,
	})
	res.Freeze()
	return res, nil
}

func parseCustomNewLines(v starlark.Value) (starlark.Value, error) {
	if v == nil || v == starlark.None {
		return starlark.None, nil
	}
	switch linesVal := v.(type) {
	case starlark.Mapping:
		it, ok := linesVal.(starlark.IterableMapping)
		if !ok {
			return nil, errors.New("for parameter \"new_lines\": mapping must be iterable")
		}
		type linePair struct {
			num  int
			text string
		}
		var pairs []linePair
		for _, kv := range it.Items() {
			lineNum, lineStr, err := parseLinePair(kv[0], kv[1])
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, linePair{num: lineNum, text: string(lineStr)})
		}
		slices.SortFunc(pairs, func(a, b linePair) int { return cmp.Compare(a.num, b.num) })
		out := make(starlark.Tuple, len(pairs))
		for i, p := range pairs {
			out[i] = starlark.Tuple{starlark.MakeInt(p.num), starlark.String(p.text)}
		}
		return out, nil
	case starlark.Sequence:
		out := make(starlark.Tuple, 0, linesVal.Len())
		iter := linesVal.Iterate()
		defer iter.Done()
		var elem starlark.Value
		for iter.Next(&elem) {
			seq, ok := elem.(starlark.Sequence)
			if !ok || seq.Len() != 2 {
				return nil, errors.New("for parameter \"new_lines\": sequence elements must be (line_number, text) pairs")
			}
			subIter := seq.Iterate()
			var first, second starlark.Value
			subIter.Next(&first)
			subIter.Next(&second)
			subIter.Done()
			lineNum, lineStr, err := parseLinePair(first, second)
			if err != nil {
				return nil, err
			}
			out = append(out, starlark.Tuple{starlark.MakeInt(lineNum), lineStr})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("for parameter \"new_lines\": got %s, want sequence of (int, str) or dict of {int: str}", v.Type())
	}
}

// parseLinePair validates a single (line_number, text) entry of the new_lines
// parameter of testing.file().
func parseLinePair(numVal, textVal starlark.Value) (int, starlark.String, error) {
	numInt, ok := numVal.(starlark.Int)
	if !ok {
		return 0, "", fmt.Errorf("for parameter \"new_lines\": line number must be int, got %s", numVal.Type())
	}
	lineNum := intToInt(numInt)
	if lineNum <= 0 {
		return 0, "", fmt.Errorf("for parameter \"new_lines\": line numbers are 1-based, got %s", numVal.String())
	}
	lineStr, ok := textVal.(starlark.String)
	if !ok {
		return 0, "", fmt.Errorf("for parameter \"new_lines\": line content must be str, got %s", textVal.Type())
	}
	return lineNum, lineStr, nil
}

func testingCommit(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var arghash starlark.String = "0000000000000000000000000000000000000000"
	var argmessage starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"hash?", &arghash,
		"message?", &argmessage,
	); err != nil {
		return nil, err
	}
	if arghash == "" {
		return nil, errors.New("for parameter \"hash\": must not be empty")
	}
	res := toValue("commit", starlark.StringDict{
		"hash":    arghash,
		"message": argmessage,
	})
	res.Freeze()
	return res, nil
}

func testingFinding(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argmessage starlark.String
	var arglevel starlark.String = "error"
	var argfilepath starlark.String
	var argline starlark.Int
	var argcol starlark.Int
	var argendLine starlark.Int
	var argendCol starlark.Int
	var argreplacements starlark.Sequence
	var argproperties = findingsPropertyBag{
		allowedProperties: s.allowedFindingsProperties,
	}
	var argcommitHash starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"message?", &argmessage,
		"level?", &arglevel,
		"filepath?", &argfilepath,
		"line??", &argline,
		"col??", &argcol,
		"end_line??", &argendLine,
		"end_col??", &argendCol,
		"replacements??", &argreplacements,
		"properties??", &argproperties,
		"commit_hash?", &argcommitHash,
	); err != nil {
		return nil, err
	}
	level := Level(string(arglevel))
	if !level.isValid() {
		return nil, fmt.Errorf("for parameter \"level\": got %s, want one of %q, %q or %q", arglevel, Notice, Warning, Error)
	}
	span, err := parseSpan(argline, argcol, argendLine, argendCol)
	if err != nil {
		return nil, err
	}
	var replacements []string
	if argreplacements != nil {
		replacements = sequenceToStrings(argreplacements)
		if replacements == nil {
			return nil, fmt.Errorf("for parameter \"replacements\": got %s, want sequence of str", argreplacements.Type())
		}
	}
	return newFindingStruct(
		string(argmessage),
		string(arglevel),
		string(argfilepath),
		span.Start.Line,
		span.Start.Col,
		span.End.Line,
		span.End.Col,
		replacements,
		argproperties.unpackedProperties,
		string(argcommitHash),
	), nil
}

func newFindingStruct(message, level, filepath string, line, col, endLine, endCol int, replacements []string, props map[string]string, commitHash string) starlark.Value {
	replTuple := make(starlark.Tuple, len(replacements))
	for i, r := range replacements {
		replTuple[i] = starlark.String(r)
	}
	propsDict := starlark.NewDict(len(props))
	if len(props) > 0 {
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			_ = propsDict.SetKey(starlark.String(k), starlark.String(props[k]))
		}
	}
	propsDict.Freeze()
	res := toValue("finding", starlark.StringDict{
		"col":          starlark.MakeInt(col),
		"commit_hash":  starlark.String(commitHash),
		"end_col":      starlark.MakeInt(endCol),
		"end_line":     starlark.MakeInt(endLine),
		"filepath":     starlark.String(filepath),
		"level":        starlark.String(level),
		"line":         starlark.MakeInt(line),
		"message":      starlark.String(message),
		"properties":   propsDict,
		"replacements": replTuple,
	})
	res.Freeze()
	return res
}

// virtualFile is a file passed to testing.run(). It embeds fileImpl so it
// gets the same lazily-computed metadata as a real file.
type virtualFile struct {
	fileImpl
	content     string
	customLines starlark.Value
	affected    bool
}

func newVirtualFile(path, action, content string, affected bool) *virtualFile {
	return &virtualFile{
		fileImpl: fileImpl{path: path, a: action},
		content:  content,
		affected: affected,
	}
}

type virtualSCM struct {
	files      []*virtualFile
	scmCommits []scmCommit
}

func (v *virtualSCM) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	var res []file
	for _, f := range v.files {
		if !f.affected {
			continue
		}
		if f.a == "D" && !filter.includeDeleted {
			continue
		}
		res = append(res, f)
	}
	return res, nil
}

func (v *virtualSCM) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	var res []file
	for _, f := range v.files {
		if f.a == "D" && !filter.includeDeleted {
			continue
		}
		res = append(res, f)
	}
	return res, nil
}

func (v *virtualSCM) newLines(ctx context.Context, fi file) (starlark.Value, error) {
	// fileImpl.getMetadata passes the embedded *fileImpl rather than the
	// *virtualFile, so look the file up by path.
	idx := slices.IndexFunc(v.files, func(f *virtualFile) bool { return f.path == fi.rootedpath() })
	if idx < 0 {
		return make(starlark.Tuple, 0), nil
	}
	vf := v.files[idx]
	if vf.customLines != nil && vf.customLines != starlark.None {
		return vf.customLines, nil
	}
	if vf.a == "D" {
		return make(starlark.Tuple, 0), nil
	}
	return newLinesWholeBytes([]byte(vf.content))
}

func (v *virtualSCM) commits(ctx context.Context) ([]scmCommit, error) {
	return slices.Clone(v.scmCommits), nil
}

type testReport struct {
	mu             sync.Mutex
	findings       []starlark.Value
	findingsByFile map[string][]findingToFix
	artifacts      map[string]string
}

func newTestReport() *testReport {
	return &testReport{
		findingsByFile: make(map[string][]findingToFix),
		artifacts:      make(map[string]string),
	}
}

func (r *testReport) EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string, props map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = append(r.findings, newFindingStruct(
		message,
		string(level),
		file,
		s.Start.Line,
		s.Start.Col,
		s.End.Line,
		s.End.Col,
		replacements,
		props,
		"",
	))
	if file != "" && len(replacements) == 1 {
		r.findingsByFile[file] = append(r.findingsByFile[file], findingToFix{
			check:       check,
			span:        s,
			replacement: replacements[0],
		})
	}
	return nil
}

func (r *testReport) EmitCommitMessageFinding(ctx context.Context, check string, level Level, message string, commitHash string, commitMessage string, s Span, props map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = append(r.findings, newFindingStruct(
		message,
		string(level),
		"",
		s.Start.Line,
		s.Start.Col,
		s.End.Line,
		s.End.Col,
		nil,
		props,
		commitHash,
	))
	return nil
}

func (r *testReport) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	if content == nil && root != "" {
		var err error
		content, err = os.ReadFile(filepath.Join(root, file))
		if err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artifacts[file] = string(content)
	return nil
}

func (r *testReport) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level Level, err error) {
}

// Print is never called because testingRun gives checks the test thread's
// print impl directly, so their output is attributed to the test case.
func (r *testReport) Print(ctx context.Context, check, file string, line int, message string) {
}

// withCheckBacktrace wraps testing.run() so that errors raised inside a check
// keep the check's own stack frames. Checks run on a separate thread, so
// otherwise Starlark would wrap the error in a new EvalError whose stack ends
// at the testing.run() call site, hiding where the check actually failed.
func withCheckBacktrace(impl func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		v, err := impl(th, fn, args, kwargs)
		if err == nil {
			return v, nil
		}
		var inner starlark.CallStack
		if f, ok := errors.AsType[*failure](err); ok {
			inner = f.Stack
		} else if e, ok := errors.AsType[*evalError](err); ok {
			inner = e.CallStack
		} else {
			return nil, err
		}
		// The outer stack ends with the testing.run builtin frame, which the
		// inner stack's first frame (the check function) replaces.
		outer := th.CallStack()
		if n := len(outer); n > 0 && outer[n-1].Pos.Filename() == "<builtin>" {
			outer = outer[:n-1]
		}
		return nil, &starlark.EvalError{
			Msg:       err.Error(),
			CallStack: append(outer, inner...),
		}
	}
}

func testingRun(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := getContext(th)
	s := ctxShacState(ctx)
	var argcheck starlark.Value
	var argfiles = starlark.NewDict(0)
	var argcommits starlark.Sequence
	var argvars = starlark.NewDict(0)
	var argcheckArgs = starlark.NewDict(0)
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"check", &argcheck,
		"files?", &argfiles,
		"commits?", &argcommits,
		"vars?", &argvars,
		"args?", &argcheckArgs,
	); err != nil {
		return nil, err
	}

	virtualFiles, err := parseVirtualFiles(argfiles)
	if err != nil {
		return nil, err
	}

	scmCommits, err := parseVirtualCommits(argcommits)
	if err != nil {
		return nil, err
	}

	vars := maps.Clone(s.vars)
	if vars == nil {
		vars = make(map[string]string, argvars.Len())
	}
	for _, kv := range argvars.Items() {
		k, ok := kv[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("for parameter \"vars\": key must be str, got %s", kv[0].Type())
		}
		v, ok := kv[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("for parameter \"vars\": value must be str, got %s", kv[1].Type())
		}
		vars[string(k)] = string(v)
	}

	testRootDir, err := s.newTempDir()
	if err != nil {
		return nil, err
	}
	if err = materializeTestRoot(testRootDir, virtualFiles); err != nil {
		return nil, err
	}
	scmRootSlash := filepath.ToSlash(testRootDir)

	// Prints from the check under test belong in the calling test's output,
	// so reuse the test thread's print impl.
	pi := th.Print

	rep := newTestReport()
	vscm := &virtualSCM{
		files:      virtualFiles,
		scmCommits: scmCommits,
	}

	cConfig := s.shacConfig
	cConfig.r = rep
	cConfig.root = testRootDir
	cConfig.tmpdir = testRootDir + "-tmp"
	cConfig.vars = vars
	cConfig.scm = vscm
	// Unlike the test file itself, testing.run() executes whatever checks get
	// registered.
	cConfig.forbidRegisterCheck = false
	cState := &shacState{
		shacConfig: cConfig,
	}
	cCtx := context.WithValue(ctx, &shacStateCtxKey, cState)

	checksToRun, err := resolveChecksToRun(cCtx, th, cState, argcheck, argcheckArgs)
	if err != nil {
		return nil, err
	}
	cState.doneLoading = true

	ctxVal, err := getCtx(scmRootSlash, cState.vars)
	if err != nil {
		return nil, err
	}
	callArgs := starlark.Tuple{ctxVal}
	callArgs.Freeze()
	for _, rc := range checksToRun {
		if err = rc.call(cCtx, cState.env, callArgs, pi); err != nil {
			return nil, err
		}
	}

	resultFiles := starlark.NewDict(len(virtualFiles))
	for _, vf := range virtualFiles {
		if vf.a == "D" {
			continue
		}
		content := vf.content
		if fileFindings := rep.findingsByFile[vf.path]; len(fileFindings) > 0 {
			var fixErr error
			content, _, fixErr = applyReplacements(content, fileFindings)
			if fixErr != nil {
				return nil, fixErr
			}
		}
		_ = resultFiles.SetKey(starlark.String(vf.path), starlark.String(content))
	}
	resultFiles.Freeze()

	artifactsDict := starlark.NewDict(len(rep.artifacts))
	artifactKeys := make([]string, 0, len(rep.artifacts))
	for k := range rep.artifacts {
		artifactKeys = append(artifactKeys, k)
	}
	slices.Sort(artifactKeys)
	for _, k := range artifactKeys {
		_ = artifactsDict.SetKey(starlark.String(k), starlark.String(rep.artifacts[k]))
	}
	artifactsDict.Freeze()

	res := toValue("result", starlark.StringDict{
		"artifacts": artifactsDict,
		"files":     resultFiles,
		"findings":  starlark.Tuple(rep.findings),
	})
	res.Freeze()
	return res, nil
}

func resolveChecksToRun(ctx context.Context, th *starlark.Thread, cState *shacState, target starlark.Value, checkArgs *starlark.Dict) ([]*registeredCheck, error) {
	var extraKwargs []starlark.Tuple
	for _, kv := range checkArgs.Items() {
		k, ok := kv[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("for parameter \"args\": key must be str, got %s", kv[0].Type())
		}
		extraKwargs = append(extraKwargs, starlark.Tuple{k, kv[1]})
	}

	var c *check
	switch x := target.(type) {
	case *check:
		c = x
	case *starlark.Function:
		if x.NumParams() == 0 && !x.HasVarargs() && !x.HasKwargs() {
			if len(extraKwargs) > 0 {
				return nil, errors.New("cannot pass \"args\" when running a 0-argument registration function")
			}
			prevCtx := th.Local("shac.context")
			th.SetLocal("shac.context", ctx)
			_, err := starlark.Call(th, x, nil, nil)
			th.SetLocal("shac.context", prevCtx)
			if err != nil {
				return nil, err
			}
			if len(cState.checks) == 0 {
				return nil, fmt.Errorf("function %q did not register any checks via shac.register_check()", x.Name())
			}
			return cState.checks, nil
		}
		var err error
		c, err = newCheck(x, "", false)
		if err != nil {
			return nil, err
		}
	case starlark.Callable:
		var err error
		c, err = newCheck(x, "", false)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("for parameter \"check\": got %s, want function or shac.check object", target.Type())
	}

	if len(extraKwargs) > 0 {
		withArgsVal, err := c.withArgs(extraKwargs)
		if err != nil {
			return nil, err
		}
		c = withArgsVal.(*check)
	}
	return []*registeredCheck{{check: c}}, nil
}

func parseVirtualFiles(filesDict *starlark.Dict) ([]*virtualFile, error) {
	var virtualFiles []*virtualFile
	for _, kv := range filesDict.Items() {
		pathVal, ok := kv[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("for parameter \"files\": key must be str, got %s", kv[0].Type())
		}
		rel := string(pathVal)
		if strings.Contains(rel, "\\") || path.IsAbs(rel) || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") || rel == ".." || rel == "." {
			return nil, fmt.Errorf("for parameter \"files\": invalid relative path %q", rel)
		}

		vf := newVirtualFile(rel, "M", "", true)
		switch v := kv[1].(type) {
		case starlark.String:
			vf.content = string(v)
		case *starlarkstruct.Struct:
			if v.Constructor() != starlark.String("file_spec") {
				return nil, fmt.Errorf("for parameter \"files\": value for %q must be str or testing.file(), got %s", rel, v.String())
			}
			actionVal, _ := v.Attr("action")
			affectedVal, _ := v.Attr("affected")
			contentVal, _ := v.Attr("content")
			newLinesVal, _ := v.Attr("new_lines")
			vf.a = string(actionVal.(starlark.String))
			vf.affected = bool(affectedVal.(starlark.Bool))
			vf.content = string(contentVal.(starlark.String))
			vf.customLines = newLinesVal
		default:
			return nil, fmt.Errorf("for parameter \"files\": value for %q must be str or testing.file(), got %s", rel, kv[1].Type())
		}
		virtualFiles = append(virtualFiles, vf)
	}
	slices.SortFunc(virtualFiles, func(a, b *virtualFile) int {
		return cmp.Compare(a.path, b.path)
	})
	return virtualFiles, nil
}

func parseVirtualCommits(seq starlark.Sequence) ([]scmCommit, error) {
	if seq == nil {
		return nil, nil
	}
	var res []scmCommit
	iter := seq.Iterate()
	defer iter.Done()
	var elem starlark.Value
	for iter.Next(&elem) {
		switch v := elem.(type) {
		case *starlarkstruct.Struct:
			hashVal, err := v.Attr("hash")
			if err != nil {
				return nil, fmt.Errorf("for parameter \"commits\": commit struct missing \"hash\"")
			}
			msgVal, err := v.Attr("message")
			if err != nil {
				return nil, fmt.Errorf("for parameter \"commits\": commit struct missing \"message\"")
			}
			h, ok1 := hashVal.(starlark.String)
			m, ok2 := msgVal.(starlark.String)
			if !ok1 || !ok2 {
				return nil, errors.New("for parameter \"commits\": commit \"hash\" and \"message\" must be str")
			}
			res = append(res, scmCommit{hash: string(h), message: string(m)})
		default:
			return nil, fmt.Errorf("for parameter \"commits\": element must be testing.commit(), got %s", elem.Type())
		}
	}
	return res, nil
}

func materializeTestRoot(testRootDir string, virtualFiles []*virtualFile) error {
	for _, vf := range virtualFiles {
		if vf.a == "D" {
			continue
		}
		dst := filepath.Join(testRootDir, filepath.FromSlash(vf.path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(vf.content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// RunTests discovers and executes Starlark *_test.star files.
func RunTests(ctx context.Context, o *Options) error {
	tmpdir, err := os.MkdirTemp("", "shac")
	if err != nil {
		return err
	}
	err = runTestsInner(ctx, tmpdir, o)
	if err2 := os.RemoveAll(tmpdir); err == nil {
		err = err2
	}
	return err
}

func runTestsInner(ctx context.Context, tmpdir string, o *Options) error {
	root, err := resolveRoot(ctx, o.Dir)
	if err != nil {
		return err
	}
	config := o.config
	if config == "" {
		config = "shac.textproto"
	}
	var doc Document
	configExists, err := loadDocument(root, config, &doc)
	if err != nil {
		return err
	}
	testFiles, err := discoverTestFiles(root, o.Files, &doc)
	if err != nil {
		return err
	}
	if len(testFiles) == 0 {
		return errors.New("no test files found")
	}

	sb, err := sandbox.New(tmpdir)
	if err != nil {
		return err
	}
	pkgMgr := NewPackageManager(tmpdir)
	packages, err := pkgMgr.RetrievePackages(ctx, root, &doc)
	if err != nil {
		return err
	}

	vars, err := resolveVars(&doc, o.Vars, config, configExists)
	if err != nil {
		return err
	}

	subprocessSem := semaphore.NewWeighted(int64(maxConcurrency))

	allowSet := make(map[string]bool, len(o.Filter.AllowList))
	for _, name := range o.Filter.AllowList {
		allowSet[name] = true
	}
	denySet := make(map[string]bool, len(o.Filter.DenyList))
	var allowedAndDenied []string
	for _, name := range o.Filter.DenyList {
		denySet[name] = true
		if allowSet[name] {
			allowedAndDenied = append(allowedAndDenied, name)
		}
	}
	if len(allowedAndDenied) > 0 {
		return fmt.Errorf("tests cannot be both allowed and denied: %s", strings.Join(allowedAndDenied, ", "))
	}
	// Filter entries that match no test are most likely typos, which would
	// otherwise silently run (or skip) the wrong set of tests.
	unmatchedFilters := make(map[string]bool, len(allowSet)+len(denySet))
	for name := range allowSet {
		unmatchedFilters[name] = true
	}
	for name := range denySet {
		unmatchedFilters[name] = true
	}

	type testCase struct {
		displayName string
		fn          *starlark.Function
		state       *shacState
		sk          sourceKey
	}
	var allTests []testCase

	for idx, relPath := range testFiles {
		env := &starlarkEnv{
			globals:     getPredeclared(),
			testGlobals: getTestPredeclared(),
			sources:     map[string]*loadedSource{},
			packages:    packages,
			opts:        starlarkOptions(),
		}
		fileTmpDir := filepath.Join(tmpdir, fmt.Sprintf("testfile-%d", idx))
		if err := os.MkdirAll(fileTmpDir, 0o700); err != nil {
			return err
		}
		s := &shacState{
			shacConfig: shacConfig{
				env:                       env,
				allowNetwork:              doc.AllowNetwork,
				writableRoot:              doc.WritableRoot,
				root:                      root,
				vars:                      vars,
				tmpdir:                    fileTmpDir,
				sandbox:                   sb,
				passthroughEnv:            doc.PassthroughEnv,
				allowedFindingsProperties: doc.allowedFindingsProperties(),
				subprocessSem:             subprocessSem,
				forbidRegisterCheck:       true,
			},
		}
		loadCtx := context.WithValue(ctx, &shacStateCtxKey, s)
		sk := sourceKey{orig: relPath, pkg: "__main__", relpath: relPath}
		pi := func(th *starlark.Thread, msg string) {
			pos := th.CallFrame(1).Pos
			if o.TestReporter != nil {
				o.TestReporter.Print(loadCtx, pos.Filename(), int(pos.Line), msg)
			}
		}
		globals, loadErr := env.load(loadCtx, sk, pi)
		if loadErr != nil {
			if s.failErr != nil {
				return s.failErr
			}
			if evalErr, ok := errors.AsType[*starlark.EvalError](loadErr); ok {
				return &evalError{evalErr}
			}
			return loadErr
		}

		var fnNames []string
		for name, val := range globals {
			if strings.HasPrefix(name, "test_") {
				if _, ok := val.(*starlark.Function); !ok {
					return fmt.Errorf("%s: %q has test_ prefix but is %s, want function", relPath, name, val.Type())
				}
				fnNames = append(fnNames, name)
			}
		}
		slices.Sort(fnNames)
		if len(fnNames) == 0 {
			return fmt.Errorf("%s: no test_ functions found", relPath)
		}

		for _, fnName := range fnNames {
			fn := globals[fnName].(*starlark.Function)
			if fn.NumParams() != 0 {
				return fmt.Errorf("%s: %s must take 0 arguments, got %d", relPath, fnName, fn.NumParams())
			}
			fullName := relPath + ":" + fnName
			displayName := fnName
			if len(testFiles) > 1 {
				displayName = fullName
			}
			delete(unmatchedFilters, fullName)
			delete(unmatchedFilters, fnName)
			if len(allowSet) > 0 && !allowSet[fullName] && !allowSet[fnName] {
				continue
			}
			if denySet[fullName] || denySet[fnName] {
				continue
			}
			allTests = append(allTests, testCase{
				displayName: displayName,
				fn:          fn,
				state:       s,
				sk:          sk,
			})
		}
	}

	if len(unmatchedFilters) > 0 {
		names := slices.Sorted(maps.Keys(unmatchedFilters))
		msg := "test does not exist"
		if len(names) > 1 {
			msg = "tests do not exist"
		}
		return fmt.Errorf("%s: %s", msg, strings.Join(names, ", "))
	}
	if len(allTests) == 0 {
		return errors.New("no tests matched filter")
	}

	type testOutcome struct {
		name   string
		file   string
		d      time.Duration
		err    error
		prints []string
	}
	outcomes := make([]testOutcome, len(allTests))
	var eg errgroup.Group
	// Bound concurrency like `shac check` does, since each test case may
	// create temporary directories and spawn subprocesses.
	eg.SetLimit(maxConcurrency)
	for i, tc := range allTests {
		eg.Go(func() error {
			start := time.Now()
			var prints []string
			pi := func(th *starlark.Thread, msg string) {
				pos := th.CallFrame(1).Pos
				prints = append(prints, fmt.Sprintf("[%s:%d] %s", pos.Filename(), pos.Line, msg))
			}
			// Give each test case its own shacState so failErr and tmpdir are
			// isolated across concurrent test functions.
			caseConfig := tc.state.shacConfig
			caseConfig.tmpdir = filepath.Join(tc.state.tmpdir, fmt.Sprintf("case-%d", i))
			caseState := &shacState{
				shacConfig: caseConfig,
			}
			caseCtx := context.WithValue(ctx, &shacStateCtxKey, caseState)
			th := caseState.env.thread(caseCtx, tc.displayName, pi)
			th.Load = func(th *starlark.Thread, str string) (starlark.StringDict, error) {
				skn, loadErr := parseSourceKey(th.Local("shac.pkg").(sourceKey), str)
				if loadErr != nil {
					return nil, loadErr
				}
				return caseState.env.loadInner(th, skn)
			}
			th.SetLocal("shac.top", tc.sk)
			th.SetLocal("shac.pkg", tc.sk)
			_, callErr := starlark.Call(th, tc.fn, nil, nil)
			if callErr != nil {
				if caseState.failErr != nil {
					callErr = caseState.failErr
				} else if evalErr, ok := errors.AsType[*starlark.EvalError](callErr); ok {
					callErr = &evalError{evalErr}
				}
			}
			outcomes[i] = testOutcome{
				name:   tc.displayName,
				file:   tc.sk.relpath,
				d:      time.Since(start),
				err:    callErr,
				prints: prints,
			}
			return nil
		})
	}
	_ = eg.Wait()

	failed := false
	for _, out := range outcomes {
		if out.err != nil {
			failed = true
		}
		if o.TestReporter != nil {
			o.TestReporter.TestResult(ctx, out.name, out.file, out.d, out.err, out.prints)
		}
	}
	if failed {
		return ErrCheckFailed
	}
	return nil
}

func discoverTestFiles(root string, files []string, doc *Document) ([]string, error) {
	if len(files) > 0 {
		// Like `shac check`, resolve relative paths against the working
		// directory rather than root, which may be an ancestor of it.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cleanRoot := filepath.Clean(root)
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, orig := range files {
			abs := orig
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(cwd, abs)
			}
			fi, err := os.Stat(abs)
			if err != nil {
				return nil, err
			}
			if fi.IsDir() {
				return nil, fmt.Errorf("is a directory: %s", orig)
			}
			// Check lexical containment first so symlinked *_test.star files
			// inside a staged/runfiles root (whose targets live outside root)
			// are kept at their root-relative paths.
			rel, err := filepath.Rel(cleanRoot, filepath.Clean(abs))
			if err != nil || !filepath.IsLocal(rel) {
				resolved, evalErr := filepath.EvalSymlinks(abs)
				if evalErr != nil {
					return nil, evalErr
				}
				rel, err = filepath.Rel(resolvedRoot, resolved)
				if err != nil {
					return nil, err
				}
				if !filepath.IsLocal(rel) {
					return nil, fmt.Errorf("cannot run test file outside root: %s", orig)
				}
			}
			slashRel := filepath.ToSlash(rel)
			out = append(out, slashRel)
		}
		slices.Sort(out)
		return slices.Compact(out), nil
	}

	var patterns []gitignore.Pattern
	for _, p := range doc.Ignore {
		if p != "" {
			patterns = append(patterns, gitignore.ParsePattern(p, nil))
		}
	}
	for _, p := range doc.IgnoreTests {
		if p != "" {
			patterns = append(patterns, gitignore.ParsePattern(p, nil))
		}
	}
	matcher := gitignore.NewMatcher(patterns)

	var discovered []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		parts := strings.Split(slashRel, "/")
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".tools" || matcher.Match(parts, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.star") {
			return nil
		}
		if matcher.Match(parts, false) {
			return nil
		}
		discovered = append(discovered, slashRel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(discovered)
	return discovered, nil
}
