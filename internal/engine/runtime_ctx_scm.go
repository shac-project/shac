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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"go.starlark.net/starlark"
)

// TODO(maruel): Would eventually support other source controls. For now all
// the projects we care about are on git.

// commitRef represents a commit.
type commitRef struct {
	// hash is the commit hash. It is normally a hex encoded SHA-1 digest for git
	// and mercurial until they switch algorithm.
	hash string
	// reference, which can be a git tag, branch name or other human readable
	// reference as relevant to the SCM.
	ref string
}

// file is one tracked file.
type file struct {
	// path is the relative path of the file, POSIX style.
	path string
	// action is one of "A", "M", etc.
	action string
}

// scmCheckout is the generic interface for version controlled sources.
//
// Returned files must be sorted.
type scmCheckout interface {
	affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error)
	allFiles(ctx context.Context, includeDeleted bool) ([]file, error)
	newLines(f file) builtin
}

// subdirSCM is a scmCheckout that only reports files from a subdirectory.
type subdirSCM struct {
	s scmCheckout
	// subdir is the subdirectory to filter on. It must be a POSIX path.
	// It must be non-empty and end with "/".
	subdir string
}

func (s *subdirSCM) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	f, err := s.s.affectedFiles(ctx, includeDeleted)
	f = s.filterFiles(f)
	// TODO(maruel): Cache.
	return f, err
}

func (s *subdirSCM) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	f, err := s.s.allFiles(ctx, includeDeleted)
	f = s.filterFiles(f)
	// TODO(maruel): Cache.
	return f, err
}

// filterFiles returns the list of files that are applicable for this subdir.
func (s *subdirSCM) filterFiles(f []file) []file {
	c := 0
	for i := range f {
		if strings.HasPrefix(f[i].path, s.subdir) {
			c++
		}
	}
	if c == len(f) {
		// Save a copy.
		return f
	}
	out := make([]file, 0, c)
	l := len(s.subdir)
	for i := range f {
		if strings.HasPrefix(f[i].path, s.subdir) {
			out = append(out, file{path: f[i].path[l:], action: f[i].action})
		}
	}
	return out
}

func (s *subdirSCM) newLines(f file) builtin {
	f.path = s.subdir + f.path
	return s.s.newLines(f)
}

// Git support.

// getSCM returns the scmCheckout implementation relevant for directory root.
//
// root is must be a clean path.
func getSCM(ctx context.Context, root string, allFiles bool) (scmCheckout, error) {
	// Flip to POSIX style path.
	root = strings.ReplaceAll(root, string(os.PathSeparator), "/")
	g := &gitCheckout{returnAll: allFiles}
	err := g.init(ctx, root)
	if err == nil {
		if g.checkoutRoot != root {
			if !strings.HasPrefix(root, g.checkoutRoot) {
				// Fix both of these issues:
				// - macOS, where $TMPDIR is a symlink or path case is different.
				// - Windows, where path case is different.
				if root, err = filepath.EvalSymlinks(root); err != nil {
					return nil, err
				}
				if g.checkoutRoot, err = filepath.EvalSymlinks(g.checkoutRoot); err != nil {
					return nil, err
				}
			}
			// Offset accordingly.
			if g.checkoutRoot != root {
				// The API and git talks POSIX path, so use that.
				subdir := root[len(g.checkoutRoot)+1:] + "/"
				return &subdirSCM{s: g, subdir: subdir}, nil
			}
		}
		return g, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		log.Printf("git not detected on $PATH")
	} else if strings.Contains(err.Error(), "not a git repository") {
		log.Printf("current working directory is not a git repository")
	} else {
		// Any other error is fatal, `g.err` will be set and cause execution to
		// stop the next time `g.run` is called.
		return nil, g.err
	}
	// TODO(maruel): Add the scm of your choice.
	return &rawTree{root: root}, nil
}

// gitCheckout represents a git checkout.
type gitCheckout struct {
	// Configuration.
	env       []string
	returnAll bool

	// Detected environment at initialization.
	// checkoutRoot is a POSIX path.
	checkoutRoot string
	head         commitRef
	upstream     commitRef

	// Late initialized information.
	mu       sync.Mutex
	modified []file       // modified files in this checkout.
	all      []file       // all files in the repo.
	err      error        // save error.
	b        bytes.Buffer // used by run().
}

func (g *gitCheckout) init(ctx context.Context, root string) error {
	// Find root.
	g.checkoutRoot = root
	g.checkoutRoot = g.run(ctx, "rev-parse", "--show-toplevel")
	// root will have normal Windows path but git returns a POSIX style path
	// that may be incorrect. Clean it up.
	g.checkoutRoot = strings.ReplaceAll(filepath.Clean(g.checkoutRoot), string(os.PathSeparator), "/")
	g.head.hash = g.run(ctx, "rev-parse", "HEAD")
	g.head.ref = g.run(ctx, "rev-parse", "--abbrev-ref=strict", "--symbolic-full-name", "HEAD")
	if g.err != nil {
		// Not worth continuing.
		return g.err
	}
	// Determine pristine status but ignoring untracked files. We do not
	// distinguish between indexed or not.
	isPristine := g.run(ctx, "status", "--porcelain", "--untracked-files=no") == ""
	g.upstream.hash = g.run(ctx, "rev-parse", "@{u}")
	if g.err != nil {
		const noUpstream = "no upstream configured for branch"
		const noBranch = "HEAD does not point to a branch"
		if s := g.err.Error(); strings.Contains(s, noUpstream) || strings.Contains(s, noBranch) {
			g.err = nil
			// If @{u} is undefined, silently default to use HEAD~1 if pristine, HEAD otherwise.
			if isPristine {
				// If HEAD~1 doesn't exist, this will fail.
				g.upstream.ref = "HEAD~1"
			} else {
				g.upstream.ref = "HEAD"
			}
			g.upstream.hash = g.run(ctx, "rev-parse", g.upstream.ref)
		}
	} else {
		g.upstream.ref = g.run(ctx, "rev-parse", "--abbrev-ref=strict", "--symbolic-full-name", "@{u}")
	}
	return g.err
}

// run runs a git command in the check. After init() is called, the mu lock is
// expected to be held.
func (g *gitCheckout) run(ctx context.Context, args ...string) string {
	if g.err != nil {
		return ""
	}
	args = append([]string{
		// Don't update the git index during read operations.
		"--no-optional-locks",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.checkoutRoot
	if g.env == nil {
		// First is for git version before 2.32, the rest are to skip the user and system config.
		g.env = append(os.Environ(), "GIT_CONFIG_NOGLOBAL=true", "GIT_CONFIG_GLOBAL=", "GIT_CONFIG_SYSTEM=", "LANG=C")
	}
	cmd.Env = g.env
	cmd.Stdout = &g.b
	cmd.Stderr = &g.b
	err := cmd.Run()
	// Always make a copy of the output, since it could be persisted. Only reuse
	// the temporary buffer.
	out := g.b.String()
	g.b.Reset()
	if err != nil {
		if errExit := (&exec.ExitError{}); errors.As(err, &errExit) {
			g.err = fmt.Errorf("error running git %s: %w\n%s", strings.Join(args, " "), err, out)
		} else {
			g.err = err
		}
	}
	return strings.TrimSpace(out)
}

// affectedFiles returns the modified files on this checkout.
//
// The entries are lazy loaded and cached.
func (g *gitCheckout) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	if g.returnAll {
		return g.allFiles(ctx, includeDeleted)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.modified == nil {
		// Each line has a variable number of NUL character, so process one at a time.
		for o := g.run(ctx, "diff", "--name-status", "-z", "-C", g.upstream.hash); len(o) != 0; {
			var action, path string
			if i := strings.IndexByte(o, 0); i != -1 {
				// For rename, ignore the percentage number.
				action = o[:1]
				o = o[i+1:]
				if i = strings.IndexByte(o, 0); i != -1 {
					path = o[:i]
					o = o[i+1:]
					if action == "C" {
						if i = strings.IndexByte(o, 0); i != -1 {
							// Ignore the source for now.
							path = o[:i]
							o = o[i+1:]
						} else {
							path = ""
						}
					} else if action == "R" {
						if i = strings.IndexByte(o, 0); i != -1 {
							// Ignore the source for now.
							path = o[:i]
							o = o[i+1:]
						} else {
							path = ""
						}
					}
				}
			}
			if path == "" {
				g.err = fmt.Errorf("missing trailing NUL character from git diff --name-status -z -C %s", g.upstream.hash)
				break
			}
			if action == "D" && !includeDeleted {
				continue
			}
			// TODO(olivernewman): Omit deleted submodules. For now they're
			// treated the same as deleted regular files.
			if action == "D" || !g.isSubmodule(path) {
				g.modified = append(g.modified, file{action: action, path: path})
			}
		}
		if g.modified == nil {
			g.modified = []file{}
		}
		sort.Slice(g.modified, func(i, j int) bool { return g.modified[i].path < g.modified[j].path })
	}
	return g.modified, g.err
}

// allFiles returns all the files in this checkout.
//
// The entries are lazy loaded and cached.
func (g *gitCheckout) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.all == nil {
		// Paths are returned in POSIX style even on Windows.
		// TODO(maruel): Extract more information.
		if o := g.run(ctx, "ls-files", "-z"); len(o) != 0 {
			items := strings.Split(o[:len(o)-1], "\x00")
			g.all = make([]file, 0, len(items))
			for _, path := range items {
				fi, err := os.Stat(filepath.Join(g.checkoutRoot, path))
				if errors.Is(err, fs.ErrNotExist) {
					if includeDeleted {
						g.all = append(g.all, file{action: "D", path: path})
					}
					continue
				} else if err != nil {
					return nil, err
				}
				if !fi.IsDir() { // Not a submodule.
					// TODO(maruel): Still include action from affectedFiles()?
					g.all = append(g.all, file{action: "A", path: path})
				}
			}
			sort.Slice(g.all, func(i, j int) bool { return g.all[i].path < g.all[j].path })
		} else {
			g.all = []file{}
		}
	}
	return g.all, g.err
}

func (g *gitCheckout) isSubmodule(path string) bool {
	fi, err := os.Stat(filepath.Join(g.checkoutRoot, path))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			g.err = err
		}
		return false
	}
	// TODO(olivernewman): Actually check the git object mode to determine if
	// it's a submodule. It would be nice to get the object mode from the git
	// command to avoid unnecessary syscalls.
	return fi.IsDir()
}

func (g *gitCheckout) newLines(f file) builtin {
	// TODO(maruel): Revisit the design, it is likely not performance efficient
	// to use a stack context.
	return func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
			return nil, err
		}
		if g.returnAll {
			// Include all lines when processing all files independent if the file
			// was modified or not.
			v, err := newLinesWhole(g.checkoutRoot, f.path)
			if err != nil {
				return nil, err
			}
			return v, nil
		}
		// Return an empty tuple for a deleted file's changed lines.
		if f.action == "D" {
			return make(starlark.Tuple, 0), nil
		}
		// TODO(maruel): It should be okay to run these concurrently.
		g.mu.Lock()
		o := g.run(ctx, "diff", "--no-prefix", "-C", "-U0", g.upstream.hash, "--", f.path)
		g.mu.Unlock()
		if o == "" {
			// TODO(maruel): This is not normal. For now fallback to the whole file.
			v, err := newLinesWhole(g.checkoutRoot, f.path)
			if err != nil {
				return nil, err
			}
			return v, nil
		}
		// Skip the header.
		for len(o) != 0 {
			done := strings.HasPrefix(o, "+++ ")
			if i := strings.Index(o, "\n"); i >= 0 {
				o = o[i+1:]
			} else {
				// Reached the end of the diff header without finding any
				// changed lines. This is probably because the file is binary,
				// so there's no meaning of "new lines" for it anyway.
				return make(starlark.Tuple, 0), nil
			}
			if done {
				break
			}
		}
		// TODO(maruel): Perf-optimize by using Index() and going on the fly
		// without creating a []string.
		items := strings.Split(o, "\n")
		c := 0
		for _, l := range items {
			if strings.HasPrefix(l, "+") {
				c++
			}
		}
		t := make(starlark.Tuple, 0, c)
		curr := 0
		for _, l := range items {
			if strings.HasPrefix(l, "@@ ") {
				// TODO(maruel): This code can panic at multiple places. Odds of this
				// happening is relatively low unless git diff goes off track.
				// @@ -171,0 +176,28 @@
				l = l[3+strings.Index(l[3:], " "):][1:]
				l = l[:strings.Index(l, " ")][1:]
				if i := strings.Index(l, ","); i > 0 {
					l = l[:i]
				}
				var err error
				if curr, err = strconv.Atoi(l); err != nil {
					panic(fmt.Sprintf("%q: %v", l, err))
				}
			} else if strings.HasPrefix(l, "+") {
				// Track the current line number.
				t = append(t, starlark.Tuple{starlark.MakeInt(curr), starlark.String(l[1:])})
				curr++
			} else if !strings.HasPrefix(l, "-") && l != "\\ No newline at end of file" {
				panic(fmt.Sprintf("unexpected line %q", l))
			}
		}
		return t, nil
	}
}

// Generic support.

type rawTree struct {
	root string

	mu  sync.Mutex
	all []file
}

func (r *rawTree) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	return r.allFiles(ctx, includeDeleted)
}

// allFiles returns all files in this directory tree.
//
// The includeDeleted argument is ignored as only files that exist on disk are
// included.
func (r *rawTree) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	if r.all == nil {
		l := len(r.root) + 1
		err = filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err2 error) error {
			if err2 == nil {
				if !d.IsDir() {
					r.all = append(r.all, file{path: path[l:]})
				}
			}
			return nil
		})
		sort.Slice(r.all, func(i, j int) bool { return r.all[i].path < r.all[j].path })
	}
	return r.all, err
}

func (r *rawTree) newLines(f file) builtin {
	// TODO(maruel): Revisit the design, it is likely not performance efficient.
	return func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
			return nil, err
		}
		v, err := newLinesWhole(r.root, f.path)
		if err != nil {
			return nil, err
		}
		return v, nil
	}
}

// Starlark adapter code.

// ctxScmAffectedFiles implements native function ctx.scm.affected_files().
//
// It returns a dictionary.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxScmAffectedFiles(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argincludeDeleted starlark.Bool
	if err := starlark.UnpackArgs(name, args, kwargs,
		"include_deleted?", &argincludeDeleted,
	); err != nil {
		return nil, err
	}
	files, err := s.scm.affectedFiles(ctx, bool(argincludeDeleted))
	if err != nil {
		return nil, err
	}
	return ctxScmFilesReturnValue(s, files), nil
}

// ctxScmAllFiles implements native function ctx.scm.all_files().
//
// It returns a dictionary.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxScmAllFiles(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argincludeDeleted starlark.Bool
	if err := starlark.UnpackArgs(name, args, kwargs,
		"include_deleted?", &argincludeDeleted,
	); err != nil {
		return nil, err
	}
	files, err := s.scm.allFiles(ctx, bool(argincludeDeleted))
	if err != nil {
		return nil, err
	}
	return ctxScmFilesReturnValue(s, files), nil
}

// ctxScmFilesReturnValue converts a list of files into a starlark.Dict to
// return from the ctx.scm.all_files() and ctx.scm.affected_files() functions.
func ctxScmFilesReturnValue(s *shacState, files []file) starlark.Value {
	out := starlark.NewDict(len(files))
	for _, f := range files {
		// Make sure to update //doc/stdlib.star whenever this function is modified.
		_ = out.SetKey(starlark.String(f.path), toValue("file", starlark.StringDict{
			"action":    starlark.String(f.action),
			"new_lines": newBuiltin("new_lines", s.scm.newLines(f)),
		}))
	}
	return out
}

// newLinesWhole returns the whole file as new lines.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func newLinesWhole(root, path string) (starlark.Value, error) {
	b, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil, err
	}
	// If the file contains a null byte we'll assume it's binary and not try to
	// parse its lines.
	if bytes.IndexByte(b, 0) != -1 {
		return make(starlark.Tuple, 0), nil
	}
	t := make(starlark.Tuple, bytes.Count(b, []byte{'\n'})+1)
	for i := range t {
		if n := bytes.IndexByte(b, '\n'); n != -1 {
			t[i] = starlark.Tuple{starlark.MakeInt(i + 1), starlark.String(unsafeString(b[:n]))}
			b = b[n+1:]
		} else {
			// Last item.
			t[i] = starlark.Tuple{starlark.MakeInt(i + 1), starlark.String(unsafeString(b))}
		}
	}
	return t, nil
}

func unsafeString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
