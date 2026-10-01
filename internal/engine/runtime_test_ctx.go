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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"go.fuchsia.dev/shac-project/shac/internal/sandbox"
	"go.starlark.net/starlark"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

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
			globals:  getPredeclared(),
			sources:  map[string]*loadedSource{},
			packages: packages,
			opts:     starlarkOptions(),
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
