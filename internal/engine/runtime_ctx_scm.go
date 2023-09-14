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
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/go-git/go-git/plumbing/format/gitignore"
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

type file interface {
	// rootedpath is the path relative to the project root.
	rootedpath() string
	// relpath is the path relative to the directory of the shac.star file being
	// executed.
	relpath() string
	action() string
	getMetadata() starlark.Value
}

// fileImpl is one tracked file.
type fileImpl struct {
	// Immutable.
	// path is the relative path of the file, POSIX style.
	path string
	// action is one of "A", "M", etc.
	a string

	// Mutable. Lazy loaded.
	mu       sync.Mutex
	metadata starlark.Value
	newLines starlark.Value
	err      error
}

func (f *fileImpl) rootedpath() string {
	return f.path
}

func (f *fileImpl) relpath() string {
	return f.path
}

func (f *fileImpl) action() string {
	return f.a
}

// getMetadata lazy loads the metadata and caches it.
//
// It also lazy load the new lines and caches them.
func (f *fileImpl) getMetadata() starlark.Value {
	f.mu.Lock()
	if f.metadata == nil {
		// Make sure to update //doc/stdlib.star whenever this function is modified.
		f.metadata = toValue("file", starlark.StringDict{
			"action": starlark.String(f.a),
			"new_lines": newBuiltin("new_lines", func(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
					return nil, err
				}
				f.mu.Lock()
				if f.newLines == nil && f.err == nil {
					f.newLines, f.err = s.scm.newLines(ctx, f)
				}
				f.mu.Unlock()
				return f.newLines, f.err
			}),
		})
	}
	m := f.metadata
	f.mu.Unlock()
	return m
}

// fileSubdirImpl is one tracked file reported as a subdirectory.
type fileSubdirImpl struct {
	file
	rel string
}

func (f *fileSubdirImpl) relpath() string {
	return f.rel
}

// scmCheckout is the generic interface for version controlled sources.
//
// Returned files must be sorted.
type scmCheckout interface {
	affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error)
	allFiles(ctx context.Context, includeDeleted bool) ([]file, error)
	newLines(ctx context.Context, f file) (starlark.Value, error)
}

type filteredSCM struct {
	matcher gitignore.Matcher
	scm     scmCheckout
}

func (f *filteredSCM) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	files, err := f.scm.affectedFiles(ctx, includeDeleted)
	return f.filter(files), err
}

func (f *filteredSCM) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	files, err := f.scm.allFiles(ctx, includeDeleted)
	return f.filter(files), err
}

func (f *filteredSCM) newLines(ctx context.Context, fi file) (starlark.Value, error) {
	return f.scm.newLines(ctx, fi)
}

// filter modifies the input slice of files in-place, removing any items that
// match one of the ignore patterns.
func (f *filteredSCM) filter(files []file) []file {
	offset := 0
	for i := 0; i+offset < len(files); {
		fi := files[i+offset]
		if f.matcher.Match(strings.Split(fi.rootedpath(), "/"), false) {
			offset++
		} else {
			i++
		}
		if i+offset < len(files) {
			files[i] = files[i+offset]
		}
	}
	return files[:len(files)-offset]
}

// overridesShacFileDirs may be implemented by scm implementations that wish to
// override the mechanism whereby shac.star files are discovered. Normally, only
// shac.star files that are included in the files returned by the scm's
// allFiles() method will be considered, but in some cases we want to have the
// scm contain a smaller set of files but still consider shac.star files that
// aren't in the scm.
type overridesShacFileDirs interface {
	scmCheckout
	// shacFileDirs returns the relative paths to directories that contain a
	// shac starlark file with the given basename.
	shacFileDirs(basename string) ([]string, error)
}

// specifiedFilesOnly is an scm that returns only a specified set of files.
type specifiedFilesOnly struct {
	files []file
	root  string
}

var _ overridesShacFileDirs = (*specifiedFilesOnly)(nil)

func (s *specifiedFilesOnly) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	return s.files, nil
}

func (s *specifiedFilesOnly) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	return s.files, nil
}

func (s *specifiedFilesOnly) newLines(ctx context.Context, f file) (starlark.Value, error) {
	// TODO(olivernewman): Use the actual scm to get the real affected lines if
	// the file is tracked.
	return newLinesWhole(s.root, f.rootedpath())
}

// shacFileDirs returns all directories containing shac.star files that apply to
// any of the listed files; i.e. every ancestor directory of one of the listed
// files that contains a shac.star file.
func (s *specifiedFilesOnly) shacFileDirs(basename string) ([]string, error) {
	dirs := map[string]struct{}{}
	for _, f := range s.files {
		for cur := path.Dir(f.rootedpath()); ; cur = path.Dir(cur) {
			dirs[cur] = struct{}{}
			if cur == "." {
				break
			}
		}
	}
	var res []string
	for dir := range dirs {
		// TODO(olivernewman): Check whether the shac.star file exists according
		// to the scm, rather than just checking whether it exists on disk, but
		// only if it's possible to do so without doing a full listing of all
		// files in the scm.
		_, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(dir), basename))
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		res = append(res, dir)
	}
	return res, nil
}

// subdirSCM is a scmCheckout that only reports files from a subdirectory.
type subdirSCM struct {
	// Immutable.
	s scmCheckout
	// subdir is the subdirectory to filter on. It must be a POSIX path.
	// It must be non-empty and end with "/".
	subdir string
}

func (s *subdirSCM) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	affected, err := s.s.affectedFiles(ctx, includeDeleted)
	affected = s.filterFiles(affected)
	return affected, err
}

func (s *subdirSCM) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	all, err := s.s.allFiles(ctx, includeDeleted)
	all = s.filterFiles(all)
	return all, err
}

// filterFiles returns the list of files that are applicable for this subdir.
func (s *subdirSCM) filterFiles(files []file) []file {
	c := 0
	for _, f := range files {
		if strings.HasPrefix(f.rootedpath(), s.subdir) {
			c++
		}
	}
	out := make([]file, 0, c)
	l := len(s.subdir)
	for _, f := range files {
		if r := f.rootedpath(); strings.HasPrefix(r, s.subdir) {
			out = append(out, &fileSubdirImpl{file: f, rel: r[l:]})
		}
	}
	return out
}

func (s *subdirSCM) newLines(ctx context.Context, f file) (starlark.Value, error) {
	return s.s.newLines(ctx, f)
}

// Git support.

// getSCM returns the scmCheckout implementation relevant for directory root.
//
// root is must be a clean path.
func getSCM(ctx context.Context, root string, allFiles bool) (scmCheckout, error) {
	// Flip to POSIX style path.
	root = strings.ReplaceAll(root, string(os.PathSeparator), "/")
	g := &gitCheckout{returnAll: allFiles, checkoutRoot: root}
	err := g.init(ctx)
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

// cachingSCM wraps any other scmCheckout and memoizes return values.
type cachingSCM struct {
	scm scmCheckout

	mu sync.Mutex
	// Mutable. Lazy loaded. Keys are `include_deleted` values.
	affected map[bool][]file
	all      map[bool][]file
}

func (c *cachingSCM) affectedFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.affected == nil {
		c.affected = make(map[bool][]file)
	}
	var err error
	if _, ok := c.affected[includeDeleted]; !ok {
		c.affected[includeDeleted], err = c.scm.affectedFiles(ctx, includeDeleted)
	}
	return c.affected[includeDeleted], err
}

func (c *cachingSCM) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.all == nil {
		c.all = make(map[bool][]file)
	}
	var err error
	if _, ok := c.all[includeDeleted]; !ok {
		c.all[includeDeleted], err = c.scm.allFiles(ctx, includeDeleted)
	}
	return c.all[includeDeleted], err
}

func (c *cachingSCM) newLines(ctx context.Context, fi file) (starlark.Value, error) {
	return c.scm.newLines(ctx, fi)
}

// gitCheckout represents a git checkout.
type gitCheckout struct {
	// Configuration.
	returnAll bool

	// Detected environment at initialization.
	// checkoutRoot is a POSIX path.
	checkoutRoot string
	head         commitRef
	upstream     commitRef

	mu  sync.Mutex
	err error // save error.
}

func (g *gitCheckout) init(ctx context.Context) error {
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
	res, err := runGitCmd(ctx, g.checkoutRoot, args...)
	if err != nil {
		g.err = err
	}
	return res
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
	// Untracked files are always considered affected (as long as they're not
	// ignored).
	o := g.run(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	var items []string
	if len(o) > 0 {
		items = strings.Split(o[:len(o)-1], "\x00")
	}
	modified := make([]file, 0, len(items))
	for _, path := range items {
		modified = append(modified, &fileImpl{a: "A", path: filepath.ToSlash(path)})
	}
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
			// `git diff` output will sometimes include newline-separated
			// non-fatal warnings at the end, e.g. if the number of renamed
			// files in the diff exceeds the `diff.renameLimit` config var.
			// If we encounter such a warning, we assume it's at the end of the
			// diff output.
			if !strings.HasPrefix(o, "warning:") {
				g.err = fmt.Errorf("missing trailing NUL character from git diff --name-status -z -C %s", g.upstream.hash)
			}
			break
		}
		if action == "D" && !includeDeleted {
			continue
		}
		// TODO(olivernewman): Omit deleted submodules. For now they're
		// treated the same as deleted regular files.
		if action == "D" || !g.isSubmodule(path) {
			// TODO(maruel): Share with allFiles.
			modified = append(modified, &fileImpl{a: action, path: filepath.ToSlash(path)})
		}
	}
	sort.Slice(modified, func(i, j int) bool { return modified[i].rootedpath() < modified[j].rootedpath() })
	return modified, g.err
}

// allFiles returns all the files in this checkout.
//
// The entries are lazy loaded and cached.
func (g *gitCheckout) allFiles(ctx context.Context, includeDeleted bool) ([]file, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Paths are returned in POSIX style even on Windows.
	// TODO(maruel): Extract more information.
	o := g.run(ctx, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if g.err != nil {
		// If an error occurred on this command or an earlier one, then the
		// ls-files output may not be parseable and we should exit early.
		return nil, g.err
	}
	items := strings.Split(o[:len(o)-1], "\x00")
	all := make([]file, 0, len(items))
	for _, path := range items {
		fi, err := os.Stat(filepath.Join(g.checkoutRoot, path))
		if errors.Is(err, fs.ErrNotExist) {
			if includeDeleted {
				all = append(all, &fileImpl{a: "D", path: filepath.ToSlash(path)})
			}
			continue
		} else if err != nil {
			return nil, err
		}
		if !fi.IsDir() { // Not a submodule.
			// TODO(maruel): Still include action from affectedFiles()?
			// TODO(maruel): Share with affectedFiles.
			all = append(all, &fileImpl{a: "A", path: filepath.ToSlash(path)})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].rootedpath() < all[j].rootedpath() })
	return all, g.err
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

func (g *gitCheckout) newLines(ctx context.Context, f file) (starlark.Value, error) {
	if g.returnAll {
		// Include all lines when processing all files independent if the file
		// was modified or not.
		v, err := newLinesWhole(g.checkoutRoot, f.rootedpath())
		if err != nil {
			return nil, err
		}
		return v, nil
	}
	// Return an empty tuple for a deleted file's changed lines.
	if f.action() == "D" {
		return make(starlark.Tuple, 0), nil
	}
	o := g.run(ctx, "diff", "--no-prefix", "-C", "-U0", "--no-ext-diff", "--irreversible-delete", g.upstream.hash, "--", f.rootedpath())
	if o == "" {
		if g.err != nil {
			return nil, g.err
		}
		// TODO(maruel): This is not normal. For now fallback to the whole file.
		v, err := newLinesWhole(g.checkoutRoot, f.rootedpath())
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
					r.all = append(r.all, &fileImpl{path: filepath.ToSlash(path[l:])})
				}
			}
			return nil
		})
		sort.Slice(r.all, func(i, j int) bool { return r.all[i].rootedpath() < r.all[j].rootedpath() })
	}
	return r.all, err
}

func (r *rawTree) newLines(ctx context.Context, f file) (starlark.Value, error) {
	return newLinesWhole(r.root, f.rootedpath())
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
		_ = out.SetKey(starlark.String(f.relpath()), f.getMetadata())
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
