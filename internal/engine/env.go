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
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type printImpl func(th *starlark.Thread, msg string)

// sourceKey is a reference as parsed by load().
type sourceKey struct {
	orig    string
	pkg     string
	relpath string
}

func (s *sourceKey) String() string {
	if s.pkg == "__main__" {
		return "//" + s.relpath
	}
	return "@" + s.pkg + "//" + s.relpath
}

func parseSourceKey(parent sourceKey, s string) (sourceKey, error) {
	sk := sourceKey{orig: s}
	if s == "" {
		return sk, errors.New("empty reference")
	}
	// It is an external reference.
	if strings.HasPrefix(s, "@") {
		parts := strings.SplitN(s[1:], "//", 2)
		sk.pkg = parts[0]
		if len(parts) == 1 {
			// The root of a package is api.star, to not conflict with self-tests that
			// would live in shac.star.
			if strings.HasSuffix(sk.pkg, "/") {
				return sk, fmt.Errorf("illegal external reference trailing \"/\": %s", s)
			}
			sk.relpath = "api.star"
		} else {
			// A file within the package is referenced.
			if len(parts[1]) == 0 {
				return sk, fmt.Errorf("illegal external reference path empty: %s", s)
			}
			for _, p := range strings.SplitN(parts[1], "/", -1) {
				if p == "internal" {
					return sk, fmt.Errorf("illegal external reference path containing \"internal\": %s", s)
				}
				if p == ".." {
					return sk, fmt.Errorf("illegal external reference path containing \"..\": %s", s)
				}
				if p == "" {
					return sk, fmt.Errorf("illegal external reference path containing \"//\": %s", s)
				}
			}
			sk.relpath = parts[1]
		}
		return sk, nil
	}

	// It is an internal reference.
	sk.pkg = parent.pkg
	if strings.HasPrefix(s, "//") {
		// It is root relative.
		sk.relpath = s[2:]
	} else {
		// It is path relative.
		sk.relpath = path.Clean(path.Join(path.Dir(parent.relpath), s))
	}
	return sk, nil
}

// loadedSource is the outcome of a load() statement.
type loadedSource struct {
	// th is the starlark thread that loaded this source file.
	th *starlark.Thread

	mu      sync.Mutex
	globals starlark.StringDict
	err     error
}

// starlarkEnv is the running environment enabling to run multiple starlark
// files in parallel.
type starlarkEnv struct {
	// Immutable.
	// globals is available to all load() statements. They must be frozen via
	// Freeze().
	globals starlark.StringDict
	// testGlobals, if set, replaces globals for files named `*_test.star`. It
	// is only set under `shac test`. It must be frozen via Freeze().
	testGlobals starlark.StringDict
	// packages are all the available packages. It must include __main__.
	packages map[string]fs.FS
	// Options for parsing Starlark.
	opts *syntax.FileOptions

	// Mutable.
	mu sync.Mutex
	// sources are the processed sources. Augments as more sources are added.
	sources map[string]*loadedSource
}

// thread returns a new starlark thread.
//
// load() statement is not allowed.
func (e *starlarkEnv) thread(ctx context.Context, name string, pi printImpl) *starlark.Thread {
	t := &starlark.Thread{Name: name, Print: pi}
	t.SetLocal("shac.context", ctx)
	return t
}

// getContext returns the context.Context given a starlark thread.
func getContext(t *starlark.Thread) context.Context {
	return t.Local("shac.context").(context.Context)
}

// load loads a starlark source file. It is safe to call it concurrently.
//
// A thread will be implicitly created.
func (e *starlarkEnv) load(ctx context.Context, sk sourceKey, pi printImpl) (starlark.StringDict, error) {
	// We are the root thread. Start a thread implicitly.
	t := e.thread(ctx, sk.String(), pi)
	t.Load = e.loadFrom
	t.SetLocal("shac.top", sk)
	t.SetLocal("shac.pkg", sk)
	return e.loadInner(t, sk)
}

// privateLoadPrefix is prepended to the `_`-prefixed names in a test file's
// load() statements, since the Starlark resolver unconditionally rejects
// loading private names. The colon can't appear in an identifier, so the
// mangled names can't collide with a module's real globals.
const privateLoadPrefix = "private:"

// isTestFile reports whether sk is a test file that gets the test-only
// predeclared symbols and may load() private names.
func (e *starlarkEnv) isTestFile(sk sourceKey) bool {
	return e.testGlobals != nil && strings.HasSuffix(sk.relpath, "_test.star")
}

// loadFrom implements starlark.Thread.Load, resolving str relative to the
// file executing the load() statement.
func (e *starlarkEnv) loadFrom(th *starlark.Thread, str string) (starlark.StringDict, error) {
	parent := th.Local("shac.pkg").(sourceKey)
	skn, err := parseSourceKey(parent, str)
	if err != nil {
		return nil, err
	}
	globals, err := e.loadInner(th, skn)
	if err != nil || !e.isTestFile(parent) {
		return globals, err
	}
	// Return a copy rather than adding the aliases to the cached globals so
	// that non-test files can't load private names by spelling out the
	// mangled name.
	withPrivate := make(starlark.StringDict, len(globals)*2)
	for k, v := range globals {
		withPrivate[k] = v
		if strings.HasPrefix(k, "_") {
			withPrivate[privateLoadPrefix+k] = v
		}
	}
	return withPrivate, nil
}

// execFile parses and executes a Starlark file. If sk is a test file, its
// load() statements may load private names.
func (e *starlarkEnv) execFile(th *starlark.Thread, sk sourceKey, src syntax.FilePortion, predeclared starlark.StringDict) (starlark.StringDict, error) {
	if !e.isTestFile(sk) {
		return starlark.ExecFileOptions(e.opts, th, sk.String(), src, predeclared)
	}
	f, err := e.opts.Parse(sk.String(), src, 0)
	if err != nil {
		return nil, err
	}
	// The resolver only allows load() statements at the top level.
	for _, stmt := range f.Stmts {
		load, ok := stmt.(*syntax.LoadStmt)
		if !ok {
			continue
		}
		for i, from := range load.From {
			if strings.HasPrefix(from.Name, "_") {
				// Replace rather than rename the identifier because the
				// parser shares it between From and To for load("m", "name"),
				// and the local binding must keep the unmangled name.
				load.From[i] = &syntax.Ident{NamePos: from.NamePos, Name: privateLoadPrefix + from.Name}
			}
		}
	}
	prog, err := starlark.FileProgram(f, predeclared.Has)
	if err != nil {
		return nil, err
	}
	g, err := prog.Init(th, predeclared)
	g.Freeze()
	if evalErr, ok := errors.AsType[*starlark.EvalError](err); ok && strings.HasPrefix(evalErr.Msg, "load: name ") {
		// Don't leak the mangled names into errors from this file's own
		// load() statements. Errors from nested loads start with "cannot
		// load" and are left alone, since mangled names are only valid here.
		evalErr.Msg = strings.ReplaceAll(evalErr.Msg, privateLoadPrefix, "")
	}
	return g, err
}

func (e *starlarkEnv) loadInner(th *starlark.Thread, sk sourceKey) (starlark.StringDict, error) {
	key := sk.String()
	e.mu.Lock()
	if source, ok := e.sources[key]; ok {
		// source has already been loaded or is in the process of being loaded
		// by another thread.

		e.mu.Unlock()
		if source.th == th {
			// `loadInner` will not be called concurrently with the same
			// Starlark thread, so if the current thread already has a lock on
			// this source it means there's a dependency cycle that would cause
			// a deadlock.
			if !source.mu.TryLock() {
				return nil, fmt.Errorf("%s was loaded in a cycle dependency graph", sk.String())
			}
		} else {
			// The source may be concurrently processed by another starlark
			// thread that's traversing the dependency graph of a separate
			// shac.star file, wait for the processing to complete by taking the
			// lock.
			//
			// This block is hard to cover since it's a race condition. We'd
			// have to inject a builtin that would hang the starlark execution
			// to force concurrency.
			source.mu.Lock()
		}
		defer source.mu.Unlock()
		return source.globals, source.err
	}

	source := &loadedSource{th: th, err: fmt.Errorf("load(%q) failed: panic while loading", sk)}
	// We are the "master" of this file, since we will be loading it. Make sure
	// others won't load it.
	source.mu.Lock()
	e.sources[key] = source
	e.mu.Unlock()
	defer source.mu.Unlock()

	// It's a new file that wasn't load'ed yet. ss.mu is still held which will
	// block concurrent loading.
	if pkg := e.packages[sk.pkg]; pkg != nil {
		if f, err := pkg.Open(sk.relpath); err == nil {
			var d []byte
			if d, err = io.ReadAll(f); err == nil {
				oldsk := th.Local("shac.pkg").(sourceKey)
				th.SetLocal("shac.pkg", sk)
				fp := syntax.FilePortion{Content: d, FirstLine: 1, FirstCol: 1}
				globals := e.globals
				if e.isTestFile(sk) {
					globals = e.testGlobals
				}
				source.globals, source.err = e.execFile(th, sk, fp, globals)
				th.SetLocal("shac.pkg", oldsk)
				if errl, ok := errors.AsType[resolve.ErrorList](source.err); ok {
					// Unwrap the error, only keep the first one.
					source.err = errl[0]
				}
				if errre, ok := errors.AsType[resolve.Error](source.err); ok {
					// Synthesize a BacktraceableError since it's nicer for the
					// user. Sadly we can't get the function context even if the
					// error is within a function implementation, so hardcode
					// "<toplevel>".
					source.err = &failure{
						Message: errre.Msg,
						Stack: starlark.CallStack{
							starlark.CallFrame{
								Name: "<toplevel>",
								Pos:  errre.Pos,
							},
						},
					}
				}
			} else {
				source.err = err
			}
			if err = f.Close(); source.err == nil {
				source.err = err
			}
		} else if errors.Is(err, fs.ErrNotExist) {
			// Hide the underlying error for determinism.
			source.err = fmt.Errorf("%s not found", sk.relpath)
		} else {
			source.err = err
		}
	} else {
		source.err = errors.New("package not found")
	}
	return source.globals, source.err
}
