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
	"io/fs"
	"os"
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
