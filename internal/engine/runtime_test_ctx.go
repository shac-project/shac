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
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
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
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const testingRootPlaceholder = "/__shac_test_root__"

// anyCmdArg is a sentinel value used as testing.any to match any single
// command-line argument in testing.exec_mock().
var anyCmdArg = toValue("any", starlark.StringDict{})

type mockExitError struct {
	code int
}

func (e *mockExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

func (e *mockExitError) ExitCode() int {
	return e.code
}

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
		"any":         anyCmdArg,
		"commit":      newBuiltin("testing.commit", testingCommit),
		"exec_mock":   newBuiltin("testing.exec_mock", testingExecMock),
		"exec_result": newBuiltin("testing.exec_result", testingExecResult),
		"file":        newBuiltin("testing.file", testingFile),
		"finding":     newBuiltin("testing.finding", testingFinding),
		"root":        starlark.String(testingRootPlaceholder),
		"run":         starlark.NewBuiltin("testing.run", testingRun),
		"write_file":  newBuiltinNone("testing.write_file", testingWriteFile),
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
	switch c := container.(type) {
	case starlark.String:
		sub, ok := item.(starlark.String)
		if !ok {
			return fmt.Errorf("for parameter \"item\": got %s, want str when container is str", item.Type())
		}
		if !strings.Contains(string(c), string(sub)) {
			return assertionFailure(msg, "%s does not contain %s", c.String(), sub.String())
		}
		return nil
	case starlark.Mapping:
		_, found, err := c.Get(item)
		if err != nil {
			return err
		}
		if !found {
			return assertionFailure(msg, "%s does not contain key %s", container.String(), item.String())
		}
		return nil
	case starlark.Iterable:
		iter := c.Iterate()
		defer iter.Done()
		var elem starlark.Value
		for iter.Next(&elem) {
			eq, err := starlark.Equal(elem, item)
			if err != nil {
				return err
			}
			if eq {
				return nil
			}
		}
		return assertionFailure(msg, "%s does not contain %s", container.String(), item.String())
	default:
		return fmt.Errorf("for parameter \"container\": got %s, want iterable, mapping, or str", container.Type())
	}
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
			if reErr != nil || !matched {
				return nil, fmt.Errorf("%s: expected error matching %q, got %q", fn.Name(), pattern, errText)
			}
		}
	}
	return starlark.None, nil
}

func prettyStarlarkValue(v starlark.Value, indent int) string {
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
			numInt, ok := kv[0].(starlark.Int)
			if !ok {
				return nil, fmt.Errorf("for parameter \"new_lines\": line number must be int, got %s", kv[0].Type())
			}
			lineNum := intToInt(numInt)
			if lineNum <= 0 {
				return nil, fmt.Errorf("for parameter \"new_lines\": line numbers are 1-based, got %s", kv[0].String())
			}
			lineStr, ok := kv[1].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("for parameter \"new_lines\": line content must be str, got %s", kv[1].Type())
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
			numInt, ok := first.(starlark.Int)
			if !ok {
				return nil, fmt.Errorf("for parameter \"new_lines\": line number must be int, got %s", first.Type())
			}
			lineNum := intToInt(numInt)
			if lineNum <= 0 {
				return nil, fmt.Errorf("for parameter \"new_lines\": line numbers are 1-based, got %s", first.String())
			}
			lineStr, ok := second.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("for parameter \"new_lines\": line content must be str, got %s", second.Type())
			}
			out = append(out, starlark.Tuple{starlark.MakeInt(lineNum), lineStr})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("for parameter \"new_lines\": got %s, want sequence of (int, str) or dict of {int: str}", v.Type())
	}
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

func testingExecMock(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argcmd starlark.Sequence
	var argretcode starlark.Int
	var argstdout starlark.String
	var argstderr starlark.String
	var arghandler starlark.Value = starlark.None
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cmd", &argcmd,
		"retcode?", &argretcode,
		"stdout?", &argstdout,
		"stderr?", &argstderr,
		"handler??", &arghandler,
	); err != nil {
		return nil, err
	}
	if argcmd.Len() == 0 {
		return nil, errors.New("for parameter \"cmd\": must not be empty")
	}
	cmdTuple := make(starlark.Tuple, 0, argcmd.Len())
	iter := argcmd.Iterate()
	var elem starlark.Value
	for iter.Next(&elem) {
		if elem == anyCmdArg {
			cmdTuple = append(cmdTuple, elem)
			continue
		}
		if _, ok := elem.(starlark.String); !ok {
			iter.Done()
			return nil, fmt.Errorf("for parameter \"cmd\": element must be str or testing.any, got %s", elem.Type())
		}
		cmdTuple = append(cmdTuple, elem)
	}
	iter.Done()

	retcode, ok := argretcode.Int64()
	if !ok {
		return nil, fmt.Errorf("for parameter \"retcode\": invalid int %s", argretcode)
	}
	if arghandler != starlark.None {
		if _, ok := arghandler.(starlark.Callable); !ok {
			return nil, fmt.Errorf("for parameter \"handler\": got %s, want callable", arghandler.Type())
		}
		if retcode != 0 || argstdout != "" || argstderr != "" {
			return nil, errors.New("cannot specify \"retcode\", \"stdout\", or \"stderr\" when \"handler\" is set")
		}
	}
	res := toValue("exec_mock", starlark.StringDict{
		"cmd":     cmdTuple,
		"handler": arghandler,
		"retcode": starlark.MakeInt64(retcode),
		"stderr":  argstderr,
		"stdout":  argstdout,
	})
	res.Freeze()
	return res, nil
}

func testingExecResult(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argretcode starlark.Int
	var argstdout starlark.String
	var argstderr starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"retcode?", &argretcode,
		"stdout?", &argstdout,
		"stderr?", &argstderr,
	); err != nil {
		return nil, err
	}
	retcode, ok := argretcode.Int64()
	if !ok {
		return nil, fmt.Errorf("for parameter \"retcode\": invalid int %s", argretcode)
	}
	res := toValue("completed_subprocess", starlark.StringDict{
		"retcode": starlark.MakeInt64(retcode),
		"stderr":  argstderr,
		"stdout":  argstdout,
	})
	res.Freeze()
	return res, nil
}

func testingWriteFile(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var argfilepath starlark.String
	var argcontent starlark.Value
	if err := starlark.UnpackArgs(name, args, kwargs,
		"filepath", &argfilepath,
		"content", &argcontent,
	); err != nil {
		return err
	}
	var contentStr string
	switch v := argcontent.(type) {
	case starlark.String:
		contentStr = string(v)
	case starlark.Bytes:
		contentStr = string(v)
	default:
		return fmt.Errorf("for parameter \"content\": got %s, want str or bytes", argcontent.Type())
	}
	scmRootSlash := filepath.ToSlash(filepath.Join(s.root, s.subdir))
	contentStr = strings.ReplaceAll(contentStr, testingRootPlaceholder, scmRootSlash)

	dst := strings.ReplaceAll(string(argfilepath), testingRootPlaceholder, scmRootSlash)
	if !filepath.IsAbs(dst) {
		var err error
		dst, err = absPath(dst, filepath.Join(s.root, s.subdir))
		if err != nil {
			return fmt.Errorf("for parameter \"filepath\": %s %w", argfilepath, err)
		}
	}
	cleaned := filepath.Clean(dst)
	// Restrict writes to the test's temporary directory tree so a test can never
	// overwrite files in the real repository checkout.
	if !isWithinDir(cleaned, s.tmpdir) && !isWithinDir(cleaned, s.root) {
		return fmt.Errorf("for parameter \"filepath\": %q is outside the test temporary directory", argfilepath)
	}
	if s.realRoot != "" && isWithinDir(cleaned, s.realRoot) && !isWithinDir(cleaned, s.tmpdir) {
		return fmt.Errorf("for parameter \"filepath\": %q cannot write to real repository root", argfilepath)
	}
	if err := os.MkdirAll(filepath.Dir(cleaned), 0o700); err != nil {
		return err
	}
	return os.WriteFile(cleaned, []byte(contentStr), 0o600)
}

func isWithinDir(target, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || filepath.IsLocal(rel)
}

type virtualFile struct {
	path        string
	a           string
	content     string
	customLines starlark.Value
	affected    bool

	mu          sync.Mutex
	metadata    starlark.Value
	cachedLines starlark.Value
	err         error
}

func (f *virtualFile) rootedpath() string {
	return f.path
}

func (f *virtualFile) relpath() string {
	return f.path
}

func (f *virtualFile) action() string {
	return f.a
}

func (f *virtualFile) getMetadata() starlark.Value {
	f.mu.Lock()
	if f.metadata == nil {
		f.metadata = toValue("file", starlark.StringDict{
			"action": starlark.String(f.a),
			"new_lines": newBuiltin("new_lines", func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
					return nil, err
				}
				f.mu.Lock()
				if f.cachedLines == nil && f.err == nil {
					f.cachedLines, f.err = s.scm.newLines(ctx, f)
				}
				f.mu.Unlock()
				return f.cachedLines, f.err
			}),
		})
		f.metadata.Freeze()
	}
	m := f.metadata
	f.mu.Unlock()
	return m
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
	vf, ok := fi.(*virtualFile)
	if !ok {
		return make(starlark.Tuple, 0), nil
	}
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

type execMockEntry struct {
	cmd     starlark.Tuple
	retcode int
	stdout  string
	stderr  string
	handler starlark.Callable
	used    bool
}

func (m *execMockEntry) matches(cmd []string, scmRootSlash string) bool {
	if len(cmd) != len(m.cmd) {
		return false
	}
	for i, wantVal := range m.cmd {
		if wantVal == anyCmdArg {
			continue
		}
		rawWant := string(wantVal.(starlark.String))
		wantStr := strings.ReplaceAll(rawWant, testingRootPlaceholder, scmRootSlash)
		gotStr := cmd[i]
		if i == 0 {
			// Allow matching repo-relative tool paths (such as ".tools/gobin/gosec")
			// even when the check constructs cmd[0] using ctx.scm.root.
			wantTrimmed := strings.TrimPrefix(wantStr, scmRootSlash+"/")
			gotTrimmed := strings.TrimPrefix(gotStr, scmRootSlash+"/")
			if wantTrimmed == gotTrimmed {
				continue
			}
		}
		if wantStr != gotStr {
			if !strings.Contains(rawWant, testingRootPlaceholder) || path.Clean(wantStr) != path.Clean(gotStr) {
				return false
			}
		}
	}
	return true
}

func testingRun(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := getContext(th)
	s := ctxShacState(ctx)
	var argcheck starlark.Value
	var argfiles = starlark.NewDict(0)
	var argcommits starlark.Sequence
	var argvars = starlark.NewDict(0)
	var argexecMocks starlark.Sequence
	var argcheckArgs = starlark.NewDict(0)
	var argsubdir starlark.String
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"check", &argcheck,
		"files?", &argfiles,
		"commits?", &argcommits,
		"vars?", &argvars,
		"exec_mocks?", &argexecMocks,
		"args?", &argcheckArgs,
		"subdir?", &argsubdir,
	); err != nil {
		return nil, err
	}

	subdir := string(argsubdir)
	if subdir == "." {
		subdir = ""
	}
	if subdir != "" {
		if strings.Contains(subdir, "\\") || path.IsAbs(subdir) || path.Clean(subdir) != subdir || strings.HasPrefix(subdir, "../") || subdir == ".." {
			return nil, fmt.Errorf("for parameter \"subdir\": invalid relative path %q", subdir)
		}
	}

	virtualFiles, shadowedPaths, err := parseVirtualFiles(argfiles, subdir)
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

	mocks, err := parseExecMocks(argexecMocks)
	if err != nil {
		return nil, err
	}

	var mocksMu sync.Mutex
	// runPass runs the checks once against files and returns their report.
	// If only is non-nil, just the checks whose names are in it run.
	runPass := func(files []*virtualFile, only map[string]bool, pi func(*starlark.Thread, string)) (*testReport, error) {
		testRootDir, passErr := s.newTempDir()
		if passErr != nil {
			return nil, passErr
		}
		extraMounts, passErr := materializeTestRoot(s.realRoot, testRootDir, subdir, s.writableRoot, files, shadowedPaths)
		if passErr != nil {
			return nil, passErr
		}
		testRootSlash := filepath.ToSlash(testRootDir)
		scmRootSlash := path.Join(testRootSlash, subdir)

		rep := newTestReport()
		vscm := &virtualSCM{
			files:      files,
			scmCommits: scmCommits,
		}

		var cState *shacState
		execHandler := func(execCtx context.Context, cmd []string, cwd string, env map[string]string, stdin io.Reader, raiseOnFailure bool, okRetcodes []int, tempDir string) (*subprocess, bool, error) {
			mocksMu.Lock()
			var matched *execMockEntry
			for _, m := range mocks {
				if !m.used && m.matches(cmd, scmRootSlash) {
					m.used = true
					matched = m
					break
				}
			}
			if matched == nil {
				for _, m := range mocks {
					if m.matches(cmd, scmRootSlash) {
						matched = m
						break
					}
				}
			}
			mocksMu.Unlock()
			if matched == nil {
				return nil, false, nil
			}

			retcode := matched.retcode
			stdoutStr := strings.ReplaceAll(matched.stdout, testingRootPlaceholder, scmRootSlash)
			stderrStr := strings.ReplaceAll(matched.stderr, testingRootPlaceholder, scmRootSlash)

			if matched.handler != nil {
				cmdVals := make(starlark.Tuple, len(cmd))
				for i, arg := range cmd {
					cmdVals[i] = starlark.String(arg)
				}
				handlerTh := cState.env.thread(execCtx, "exec_mock_handler", pi)
				resVal, callErr := starlark.Call(handlerTh, matched.handler, starlark.Tuple{cmdVals}, nil)
				if callErr != nil {
					return nil, true, callErr
				}
				if resVal != starlark.None {
					st, ok := resVal.(*starlarkstruct.Struct)
					if !ok || st.Constructor() != starlark.String("completed_subprocess") {
						return nil, true, fmt.Errorf("exec_mock handler must return None or testing.exec_result(), got %s", resVal.Type())
					}
					rcVal, _ := st.Attr("retcode")
					outVal, _ := st.Attr("stdout")
					errVal, _ := st.Attr("stderr")
					retcode = intToInt(rcVal.(starlark.Int))
					stdoutStr = strings.ReplaceAll(string(outVal.(starlark.String)), testingRootPlaceholder, scmRootSlash)
					stderrStr = strings.ReplaceAll(string(errVal.(starlark.String)), testingRootPlaceholder, scmRootSlash)
				}
			}

			stdoutBuf, stderrBuf := buffers.get(), buffers.get()
			stdoutBuf.WriteString(stdoutStr)
			stderrBuf.WriteString(stderrStr)

			errs := make(chan error, 1)
			if retcode != 0 {
				errs <- &mockExitError{code: retcode}
			}
			close(errs)

			return &subprocess{
				args:           cmd,
				stdout:         stdoutBuf,
				stderr:         stderrBuf,
				raiseOnFailure: raiseOnFailure,
				okRetcodes:     okRetcodes,
				tempDir:        tempDir,
				errs:           errs,
			}, true, nil
		}

		cConfig := s.shacConfig
		cConfig.r = rep
		cConfig.root = testRootDir
		cConfig.subdir = subdir
		cConfig.extraMounts = extraMounts
		cConfig.tmpdir = testRootDir + "-tmp"
		cConfig.vars = vars
		cConfig.scm = vscm
		cConfig.execHandler = execHandler
		// Unlike the test file itself, testing.run() executes whatever checks
		// get registered.
		cConfig.forbidRegisterCheck = false
		cState = &shacState{
			shacConfig: cConfig,
		}
		cCtx := context.WithValue(ctx, &shacStateCtxKey, cState)

		checksToRun, passErr := resolveChecksToRun(cCtx, th, cState, argcheck, argcheckArgs)
		if passErr != nil {
			return nil, passErr
		}
		cState.doneLoading = true

		ctxVal, passErr := getCtx(scmRootSlash, cState.vars)
		if passErr != nil {
			return nil, passErr
		}
		callArgs := starlark.Tuple{ctxVal}
		callArgs.Freeze()
		for _, rc := range checksToRun {
			if only != nil && !only[rc.check.name] {
				continue
			}
			if passErr = rc.call(cCtx, cState.env, callArgs, pi); passErr != nil {
				return nil, passErr
			}
		}
		return rep, nil
	}

	// Prints from the check under test (and from exec_mock handlers) belong
	// in the calling test's output, so reuse the test thread's print impl.
	rep, err := runPass(virtualFiles, nil, th.Print)
	if err != nil {
		return nil, err
	}

	for _, m := range mocks {
		if !m.used {
			return nil, fmt.Errorf("unused exec_mock: %s", m.cmd.String())
		}
	}

	contents, err := applyTestFixes(virtualFiles, rep, func(files []*virtualFile, only map[string]bool) (*testReport, error) {
		// Re-runs only exist to compute res.files, so their output would
		// just be confusing duplicates of the first pass's output.
		return runPass(files, only, func(*starlark.Thread, string) {})
	})
	if err != nil {
		return nil, err
	}
	resultFiles := starlark.NewDict(len(contents))
	for _, vf := range virtualFiles {
		if content, ok := contents[vf.path]; ok {
			_ = resultFiles.SetKey(starlark.String(vf.path), starlark.String(content))
		}
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

// applyTestFixes returns the contents of the non-deleted virtual files after
// applying the fixes in rep, keyed by path.
//
// Like `shac fix`, overlapping findings (including multiple findings on the
// same line) aren't applied together, since each replacement was computed
// against the original contents. Instead, the checks whose findings were
// skipped are re-run via rerun against the partially-fixed files until
// nothing is skipped, so that res.files matches what `shac fix` would write.
func applyTestFixes(files []*virtualFile, rep *testReport, rerun func([]*virtualFile, map[string]bool) (*testReport, error)) (map[string]string, error) {
	contents := make(map[string]string, len(files))
	for _, vf := range files {
		if vf.a != "D" {
			contents[vf.path] = vf.content
		}
	}
	seen := map[[sha256.Size]byte]bool{}
	for pass := 1; ; pass++ {
		skippedChecks := map[string]bool{}
		numSkipped := 0
		for _, vf := range files {
			fileFindings := rep.findingsByFile[vf.path]
			if vf.a == "D" || len(fileFindings) == 0 {
				continue
			}
			content, fr, err := applyReplacements(contents[vf.path], fileFindings)
			if err != nil {
				return nil, err
			}
			contents[vf.path] = content
			numSkipped += fr.numSkipped
			for _, c := range fr.skippedChecks {
				skippedChecks[c] = true
			}
		}
		if numSkipped == 0 {
			return contents, nil
		}

		state := make(map[string][sha256.Size]byte, len(contents))
		for p, c := range contents {
			state[p] = sha256.Sum256([]byte(c))
		}
		checkNames := slices.Sorted(maps.Keys(skippedChecks))
		fp := rerunFingerprint(state, checkNames, nil)
		if seen[fp] {
			return nil, fmt.Errorf("%d findings not fixed: fixes did not converge after %d passes", numSkipped, pass)
		}
		seen[fp] = true
		if pass >= maxFixPasses {
			return nil, fmt.Errorf("%d findings still not fixed after %d passes due to overlap with other fixes", numSkipped, pass)
		}

		nextFiles := make([]*virtualFile, 0, len(files))
		for _, vf := range files {
			// Custom new_lines describe the original contents, so let
			// re-runs compute them from the fixed contents instead.
			nextFiles = append(nextFiles, &virtualFile{
				path:     vf.path,
				a:        vf.a,
				content:  contents[vf.path],
				affected: vf.affected,
			})
		}
		var err error
		rep, err = rerun(nextFiles, skippedChecks)
		if err != nil {
			return nil, err
		}
	}
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

func parseVirtualFiles(filesDict *starlark.Dict, subdir string) ([]*virtualFile, map[string]bool, error) {
	var virtualFiles []*virtualFile
	shadowedPaths := make(map[string]bool)
	for dir := subdir; dir != "." && dir != ""; dir = path.Dir(dir) {
		shadowedPaths[dir] = true
	}
	for _, kv := range filesDict.Items() {
		pathVal, ok := kv[0].(starlark.String)
		if !ok {
			return nil, nil, fmt.Errorf("for parameter \"files\": key must be str, got %s", kv[0].Type())
		}
		rel := string(pathVal)
		if strings.Contains(rel, "\\") || path.IsAbs(rel) || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") || rel == ".." || rel == "." {
			return nil, nil, fmt.Errorf("for parameter \"files\": invalid relative path %q", rel)
		}
		for dir := path.Join(subdir, rel); dir != "." && dir != ""; dir = path.Dir(dir) {
			shadowedPaths[dir] = true
		}

		vf := &virtualFile{
			path:     rel,
			a:        "M",
			affected: true,
		}
		switch v := kv[1].(type) {
		case starlark.String:
			vf.content = string(v)
		case *starlarkstruct.Struct:
			if v.Constructor() != starlark.String("file_spec") {
				return nil, nil, fmt.Errorf("for parameter \"files\": value for %q must be str or testing.file(), got %s", rel, v.String())
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
			return nil, nil, fmt.Errorf("for parameter \"files\": value for %q must be str or testing.file(), got %s", rel, kv[1].Type())
		}
		virtualFiles = append(virtualFiles, vf)
	}
	slices.SortFunc(virtualFiles, func(a, b *virtualFile) int {
		return cmp.Compare(a.path, b.path)
	})
	return virtualFiles, shadowedPaths, nil
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

func parseExecMocks(seq starlark.Sequence) ([]*execMockEntry, error) {
	if seq == nil {
		return nil, nil
	}
	var res []*execMockEntry
	iter := seq.Iterate()
	defer iter.Done()
	var elem starlark.Value
	for iter.Next(&elem) {
		st, ok := elem.(*starlarkstruct.Struct)
		if !ok || st.Constructor() != starlark.String("exec_mock") {
			return nil, fmt.Errorf("for parameter \"exec_mocks\": element must be testing.exec_mock(), got %s", elem.Type())
		}
		cmdVal, _ := st.Attr("cmd")
		retcodeVal, _ := st.Attr("retcode")
		stdoutVal, _ := st.Attr("stdout")
		stderrVal, _ := st.Attr("stderr")
		handlerVal, _ := st.Attr("handler")
		entry := &execMockEntry{
			cmd:     cmdVal.(starlark.Tuple),
			retcode: intToInt(retcodeVal.(starlark.Int)),
			stdout:  string(stdoutVal.(starlark.String)),
			stderr:  string(stderrVal.(starlark.String)),
		}
		if handlerVal != starlark.None {
			entry.handler = handlerVal.(starlark.Callable)
		}
		res = append(res, entry)
	}
	return res, nil
}

func materializeTestRoot(realRoot, testRootDir, subdir string, writableRoot bool, virtualFiles []*virtualFile, shadowedPaths map[string]bool) ([]sandbox.Mount, error) {
	var extraMounts []sandbox.Mount
	if realRoot != "" {
		_, statErr := os.Lstat(filepath.Join(realRoot, ".git"))
		isGitRepo := statErr == nil
		seenMounts := make(map[string]bool)
		if err := populateRealRootSymlinks(realRoot, realRoot, testRootDir, "", isGitRepo, writableRoot, shadowedPaths, seenMounts, &extraMounts); err != nil {
			return nil, err
		}
	}
	if subdir != "" {
		if err := os.MkdirAll(filepath.Join(testRootDir, filepath.FromSlash(subdir)), 0o700); err != nil {
			return nil, err
		}
	}
	for _, vf := range virtualFiles {
		if vf.a == "D" {
			continue
		}
		dst := filepath.Join(testRootDir, filepath.FromSlash(subdir), filepath.FromSlash(vf.path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, []byte(vf.content), 0o600); err != nil {
			return nil, err
		}
	}
	return extraMounts, nil
}

func populateRealRootSymlinks(realRoot, realDir, testDir, relPrefix string, isGitRepo, writableRoot bool, shadowedPaths, seenMounts map[string]bool, extraMounts *[]sandbox.Mount) error {
	entries, err := os.ReadDir(realDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if relPrefix == "" && name == ".git" {
			continue
		}
		rel := name
		if relPrefix != "" {
			rel = relPrefix + "/" + name
		}
		srcPath := filepath.Join(realDir, name)
		dstPath := filepath.Join(testDir, name)
		// In a non-git staged root (such as a Bazel .runfiles/_main tree),
		// subdirectories are real directories whose leaf files are relative
		// symlinks pointing outside realRoot. Recurse into subdirectories so
		// leaf symlinks are resolved and mounted into the sandbox.
		if e.IsDir() && (shadowedPaths[rel] || !isGitRepo) {
			if err := os.MkdirAll(dstPath, 0o700); err != nil {
				return err
			}
			if err := populateRealRootSymlinks(realRoot, srcPath, dstPath, rel, isGitRepo, writableRoot, shadowedPaths, seenMounts, extraMounts); err != nil {
				return err
			}
			continue
		}
		if !shadowedPaths[rel] {
			linkTarget := srcPath
			if e.Type()&os.ModeSymlink != 0 {
				if resolved, evalErr := filepath.EvalSymlinks(srcPath); evalErr == nil {
					linkTarget = resolved
					if !isWithinDir(resolved, realRoot) && !seenMounts[resolved] {
						seenMounts[resolved] = true
						*extraMounts = append(*extraMounts, sandbox.Mount{
							Path:     resolved,
							Writable: writableRoot,
						})
					}
				}
			}
			if err := os.Symlink(linkTarget, dstPath); err != nil {
				return err
			}
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

	vars := make(map[string]string, len(doc.Vars))
	for _, v := range doc.Vars {
		vars[v.Name] = v.Default
	}
	for name, value := range o.Vars {
		if _, ok := vars[name]; !ok {
			if configExists {
				return fmt.Errorf("var not declared in %s: %s", config, name)
			}
			return fmt.Errorf("var must be declared in a %s file: %s", config, name)
		}
		vars[name] = value
	}

	var allowedFindingsProperties map[string]bool
	if doc.AllowedFindingsProperties != nil {
		allowedFindingsProperties = make(map[string]bool, len(doc.AllowedFindingsProperties.Properties))
		for _, p := range doc.AllowedFindingsProperties.Properties {
			if allowedFindingsProperties[p.Name] {
				return fmt.Errorf("cannot contain duplicate property name in allowed_findings_properties: %s", p.Name)
			}
			allowedFindingsProperties[p.Name] = true
		}
	}

	subprocessSem := semaphore.NewWeighted(int64(maxConcurrency))

	allowSet := make(map[string]bool, len(o.Filter.AllowList))
	for _, name := range o.Filter.AllowList {
		allowSet[name] = true
	}
	denySet := make(map[string]bool, len(o.Filter.DenyList))
	for _, name := range o.Filter.DenyList {
		denySet[name] = true
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
				realRoot:                  root,
				vars:                      vars,
				tmpdir:                    fileTmpDir,
				sandbox:                   sb,
				passthroughEnv:            doc.PassthroughEnv,
				allowedFindingsProperties: allowedFindingsProperties,
				subprocessSem:             subprocessSem,
				forbidRegisterCheck:       true,
			},
		}
		loadCtx := context.WithValue(ctx, &shacStateCtxKey, s)
		sk := sourceKey{orig: relPath, pkg: "__main__", relpath: relPath}
		pi := func(th *starlark.Thread, msg string) {
			pos := th.CallFrame(1).Pos
			if o.Report != nil {
				o.Report.Print(loadCtx, "", pos.Filename(), int(pos.Line), msg)
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
	for i, tc := range allTests {
		eg.Go(func() error {
			start := time.Now()
			var printsMu sync.Mutex
			var prints []string
			pi := func(th *starlark.Thread, msg string) {
				pos := th.CallFrame(1).Pos
				printsMu.Lock()
				prints = append(prints, fmt.Sprintf("[%s:%d] %s", pos.Filename(), pos.Line, msg))
				printsMu.Unlock()
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
			th.Load = caseState.env.loadFrom
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

	if len(files) > 0 {
		cleanRoot := filepath.Clean(root)
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, orig := range files {
			abs := orig
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(root, abs)
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
