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
			// The root of a package is pkg.star, to not conflict with self-tests that
			// would live in shac.star.
			if strings.HasSuffix(sk.pkg, "/") {
				return sk, fmt.Errorf("illegal external reference trailing \"/\": %s", s)
			}
			sk.relpath = "pkg.star"
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
	// packages are all the available packages. It must include __main__.
	packages map[string]fs.FS

	// Mutable.
	mu sync.Mutex
	// sources are the processed sources. Augments as more sources are added.
	sources map[string]*loadedSource
}

// thread returns a new starlark thread.
//
// load() statement is not allowed.
func (s *starlarkEnv) thread(ctx context.Context, name string, pi printImpl) *starlark.Thread {
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
func (s *starlarkEnv) load(ctx context.Context, sk sourceKey, pi printImpl) (starlark.StringDict, error) {
	// We are the root thread. Start a thread implicitly.
	t := s.thread(ctx, sk.String(), pi)
	t.Load = func(th *starlark.Thread, str string) (starlark.StringDict, error) {
		skn, err := parseSourceKey(th.Local("shac.pkg").(sourceKey), str)
		if err != nil {
			return nil, err
		}
		return s.loadInner(ctx, th, skn, pi)
	}
	t.SetLocal("shac.top", sk)
	t.SetLocal("shac.pkg", sk)
	return s.loadInner(ctx, t, sk, pi)
}

func (s *starlarkEnv) loadInner(ctx context.Context, th *starlark.Thread, sk sourceKey, pi printImpl) (starlark.StringDict, error) {
	key := sk.String()
	s.mu.Lock()
	ss, ok := s.sources[key]
	if !ok {
		ss = &loadedSource{th: th, err: fmt.Errorf("load(%q) failed: panic while loading", sk)}
		// We are the "master" of this file, since we will be loading it. Make sure
		// others won't load it.
		ss.mu.Lock()
		s.sources[key] = ss
		// It's a new file that wasn't load'ed yet. ss.mu is still held which will
		// block concurrent loading.
		defer ss.mu.Unlock()
	}
	s.mu.Unlock()

	if ok {
		if ss.th == th {
			// This source was loaded by this very thread.
			return nil, fmt.Errorf("%s was loaded in a cycle dependency graph", sk.String())
		}
		// These lines are hard to cover since it's a race condition. We'd have to
		// inject a builtin that would hang the starlark execution to force
		// concurrency.
		// The source may be concurrently processed by another starlark thread,
		// wait for the processing to complete by taking the lock.
		ss.mu.Lock()
		g := ss.globals
		err := ss.err
		ss.mu.Unlock()
		return g, err
	}

	// It's a new file that wasn't load'ed yet. ss.mu is still held which will
	// block concurrent loading.
	if pkg := s.packages[sk.pkg]; pkg != nil {
		if f, err := pkg.Open(sk.relpath); err == nil {
			var d []byte
			if d, err = io.ReadAll(f); err == nil {
				oldsk := th.Local("shac.pkg").(sourceKey)
				th.SetLocal("shac.pkg", sk)
				fp := syntax.FilePortion{Content: d, FirstLine: 1, FirstCol: 1}
				ss.globals, ss.err = starlark.ExecFile(th, sk.String(), fp, s.globals)
				th.SetLocal("shac.pkg", oldsk)
				var errl resolve.ErrorList
				if errors.As(ss.err, &errl) {
					// Unwrap the error, only keep the first one.
					ss.err = errl[0]
				}
				var errre resolve.Error
				if errors.As(ss.err, &errre) {
					// Synthesize a BacktracableError since it's nicer for the user.
					// Sadly we can't get the function context even if the error is
					// within a function implementation, so hardcode "<toplevel>".
					ss.err = &failure{
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
				ss.err = err
			}
			if err = f.Close(); ss.err == nil {
				ss.err = err
			}
		} else if errors.Is(err, fs.ErrNotExist) {
			// Hide the underlying error for determinism.
			ss.err = errors.New("file not found")
		} else {
			ss.err = err
		}
	} else {
		ss.err = errors.New("package not found")
	}
	return ss.globals, ss.err
}
