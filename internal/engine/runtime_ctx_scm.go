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
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
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

type scmCommit struct {
	hash    string
	message string
}

func (c *scmCommit) getMetadata() starlark.Value {
	return toValue("commit", starlark.StringDict{
		"hash":    starlark.String(c.hash),
		"message": starlark.String(c.message),
	})
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
		// Freeze the metadata immediately after creation while holding the
		// lock. This prevents data races later when builtinWrapper calls
		// Freeze() on dictionaries containing this shared struct instance from
		// concurrent checks. Starlark's Struct.Freeze() is not thread-safe for
		// concurrent calls on non-frozen objects.
		f.metadata.Freeze()
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

// fileFilter contains options for filtering files returned by SCM methods.
type fileFilter struct {
	includeDeleted  bool
	includeSymlinks bool
}

// scmCheckout is the generic interface for version controlled sources.
//
// Returned files must be sorted.
type scmCheckout interface {
	// affectedFiles returns the list of files that are modified or untracked
	// in the current checkout, relative to the comparison base.
	affectedFiles(ctx context.Context, filter fileFilter) ([]file, error)

	// allFiles returns all files in the checkout that shac cares about. It
	// should always return all files even when a specific file set is provided.
	allFiles(ctx context.Context, filter fileFilter) ([]file, error)

	// newLines returns a Starlark tuple of tuples representing the lines in the
	// file that are considered new/modified.
	newLines(ctx context.Context, f file) (starlark.Value, error)
	commits(ctx context.Context) ([]scmCommit, error)
}

// filteredSCM is an scmCheckout that filters files based on a
// gitignore.Matcher, in support of the `ignore` field in shac.textproto.
type filteredSCM struct {
	matcher     gitignore.Matcher
	exemptPaths []string
	scm         scmCheckout
}

var _ overridesShacFileDirs = (*filteredSCM)(nil)

func (f *filteredSCM) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	files, err := f.scm.affectedFiles(ctx, filter)
	return f.filter(files), err
}

func (f *filteredSCM) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	files, err := f.scm.allFiles(ctx, filter)
	return f.filter(files), err
}

func (f *filteredSCM) newLines(ctx context.Context, fi file) (starlark.Value, error) {
	return f.scm.newLines(ctx, fi)
}

func (f *filteredSCM) commits(ctx context.Context) ([]scmCommit, error) {
	return f.scm.commits(ctx)
}

func (f *filteredSCM) shacFileDirs(ctx context.Context, basename string) ([]string, error) {
	raw, err := shacFileDirs(ctx, f.scm, basename)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, d := range raw {
		if !f.isDirExempt(d) {
			if d != "" && f.matcher.Match(strings.Split(d, "/"), true) {
				continue
			}
			if f.matcher.Match(strings.Split(path.Join(d, basename), "/"), false) {
				continue
			}
		}
		dirs = append(dirs, d)
	}
	return dirs, nil
}

// isDirExempt reports whether directory d (relative to the root, POSIX style)
// is exempt from ignore patterns because an explicit CLI path argument is an
// ancestor of, equal to, or inside d.
func (f *filteredSCM) isDirExempt(d string) bool {
	if d == "" {
		return len(f.exemptPaths) > 0
	}
	for _, exempt := range f.exemptPaths {
		cleanExempt := strings.TrimSuffix(exempt, "/")
		isAncestor := strings.HasPrefix(cleanExempt, d+"/")
		isDescendant := isDirPath(exempt) && strings.HasPrefix(d, exempt)
		if cleanExempt == d || isAncestor || isDescendant {
			return true
		}
	}
	return false
}

// filter returns a new slice containing only files that do not match any of the
// ignore patterns.
func (f *filteredSCM) filter(files []file) []file {
	res := make([]file, 0, len(files))
	for _, fi := range files {
		p := fi.rootedpath()
		if matchesPathSpecs(f.exemptPaths, p) || !f.matcher.Match(strings.Split(p, "/"), false) {
			res = append(res, fi)
		}
	}
	return res
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
	shacFileDirs(ctx context.Context, basename string) ([]string, error)
}

// shacFileDirs returns the relative paths to directories that contain a shac
// starlark file with the given basename, using overridesShacFileDirs when
// implemented by scm to avoid a full `git ls-files` when only specific files
// were requested on the command line.
func shacFileDirs(ctx context.Context, scm scmCheckout, basename string) ([]string, error) {
	if v, ok := scm.(overridesShacFileDirs); ok {
		return v.shacFileDirs(ctx, basename)
	}
	return shacFileDirsFromAllFiles(ctx, scm, basename)
}

func shacFileDirsFromAllFiles(ctx context.Context, scm scmCheckout, basename string) ([]string, error) {
	files, err := scm.allFiles(ctx, fileFilter{includeSymlinks: true})
	if err != nil {
		return nil, err
	}
	var subdirs []string
	for _, f := range files {
		n := f.rootedpath()
		if path.Base(n) == basename {
			subdir := path.Dir(n)
			if subdir == "." {
				subdir = ""
			}
			subdirs = append(subdirs, subdir)
		}
	}
	return subdirs, nil
}

type inMemoryFile struct {
	data       []byte
	targetFile file
	root       string
	base       scmCheckout
}

var _ overridesShacFileDirs = (*inMemoryFile)(nil)

func (s *inMemoryFile) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	return []file{s.targetFile}, nil
}

func (s *inMemoryFile) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	return []file{s.targetFile}, nil
}

func (s *inMemoryFile) shacFileDirs(ctx context.Context, basename string) ([]string, error) {
	if s.base != nil {
		return shacFileDirs(ctx, s.base, basename)
	}
	return nil, nil
}

func (s *inMemoryFile) newLines(ctx context.Context, f file) (starlark.Value, error) {
	if f.rootedpath() != s.targetFile.rootedpath() {
		return nil, fmt.Errorf("file %q is not managed", f.rootedpath())
	}
	return newLinesWholeBytes(s.data)
}

func (s *inMemoryFile) commits(ctx context.Context) ([]scmCommit, error) {
	return nil, nil
}

// specifiedFilesOnly is an scm that filters affected files to a specified set
// of files and directories while keeping explicit file arguments available even
// if unmodified or ignored.
type specifiedFilesOnly struct {
	paths []string
	root  string
	base  scmCheckout
}

var _ overridesShacFileDirs = (*specifiedFilesOnly)(nil)

// addExplicitFiles returns files with any explicitly specified file arguments
// from s.paths that are not already present (e.g. because they are unmodified
// in git or excluded by .gitignore) inserted in sorted order.
func (s *specifiedFilesOnly) addExplicitFiles(files []file) []file {
	cloned := false
	for _, candidate := range s.paths {
		if isDirPath(candidate) {
			continue
		}
		idx, found := slices.BinarySearchFunc(files, candidate, func(f file, target string) int {
			return strings.Compare(f.rootedpath(), target)
		})
		if !found {
			if !cloned {
				files = slices.Clone(files)
				cloned = true
			}
			files = slices.Insert(files, idx, file(&fileImpl{path: candidate}))
		}
	}
	return files
}

func (s *specifiedFilesOnly) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	affected, err := s.base.affectedFiles(ctx, filter)
	if err != nil {
		return nil, err
	}
	var filtered []file
	for _, af := range affected {
		if matchesPathSpecs(s.paths, af.rootedpath()) {
			filtered = append(filtered, af)
		}
	}
	return s.addExplicitFiles(filtered), nil
}

func (s *specifiedFilesOnly) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	all, err := s.base.allFiles(ctx, filter)
	if err != nil {
		return nil, err
	}
	return s.addExplicitFiles(all), nil
}

func (s *specifiedFilesOnly) newLines(ctx context.Context, f file) (starlark.Value, error) {
	return s.base.newLines(ctx, f)
}

func (s *specifiedFilesOnly) commits(ctx context.Context) ([]scmCommit, error) {
	return s.base.commits(ctx)
}

// shacFileDirs returns all directories containing shac.star files that apply to
// any of the listed files or directories.
func (s *specifiedFilesOnly) shacFileDirs(ctx context.Context, basename string) ([]string, error) {
	ancestorDirs := make(map[string]bool)
	hasDirSpecs := false
	for _, spec := range s.paths {
		if isDirPath(spec) {
			hasDirSpecs = true
		}
		// Directory specs have a trailing slash (or are ""), so path.Dir(spec)
		// is the directory itself for directory specs and the parent directory
		// for file specs.
		for cur := path.Dir(spec); ; cur = path.Dir(cur) {
			if cur == "." {
				ancestorDirs[""] = true
				break
			}
			ancestorDirs[cur] = true
		}
	}

	dirs := make(map[string]bool)
	for d := range ancestorDirs {
		// Check whether the shac.star file exists on disk for ancestor
		// directories so that when only explicit files are specified we avoid a
		// full `git ls-files` call.
		fi, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(d), basename))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			dirs[d] = true
		}
	}

	if hasDirSpecs {
		// Discover descendant shac.star files via the SCM rather than walking
		// the filesystem directly, so git-ignored directories and submodules
		// are not traversed.
		allDirs, err := shacFileDirsFromAllFiles(ctx, s, basename)
		if err != nil {
			return nil, err
		}
		for _, d := range allDirs {
			if matchesPathSpecs(s.paths, path.Join(d, basename)) {
				dirs[d] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(dirs)), nil
}

// subdirSCM is a scmCheckout that only reports files from a subdirectory.
type subdirSCM struct {
	// Immutable.
	s scmCheckout
	// subdir is the subdirectory to filter on. It must be a POSIX path.
	// It must be non-empty and end with "/".
	subdir string
}

func (s *subdirSCM) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	affected, err := s.s.affectedFiles(ctx, filter)
	affected = s.filterFiles(affected)
	return affected, err
}

func (s *subdirSCM) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	all, err := s.s.allFiles(ctx, filter)
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

func (s *subdirSCM) commits(ctx context.Context) ([]scmCommit, error) {
	return s.s.commits(ctx)
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
	// Mutable. Lazy loaded.
	affected      map[fileFilter][]file
	all           map[fileFilter][]file
	commitsVal    []scmCommit
	commitsLoaded bool
	commitsErr    error
}

var _ overridesShacFileDirs = (*cachingSCM)(nil)

func (c *cachingSCM) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.affected == nil {
		c.affected = make(map[fileFilter][]file)
	}
	var err error
	if _, ok := c.affected[filter]; !ok {
		c.affected[filter], err = c.scm.affectedFiles(ctx, filter)
	}
	return c.affected[filter], err
}

func (c *cachingSCM) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.all == nil {
		c.all = make(map[fileFilter][]file)
	}
	var err error
	if _, ok := c.all[filter]; !ok {
		c.all[filter], err = c.scm.allFiles(ctx, filter)
	}
	return c.all[filter], err
}

func (c *cachingSCM) newLines(ctx context.Context, fi file) (starlark.Value, error) {
	return c.scm.newLines(ctx, fi)
}

func (c *cachingSCM) commits(ctx context.Context) ([]scmCommit, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.commitsLoaded {
		c.commitsVal, c.commitsErr = c.scm.commits(ctx)
		c.commitsLoaded = true
	}
	return c.commitsVal, c.commitsErr
}

func (c *cachingSCM) shacFileDirs(ctx context.Context, basename string) ([]string, error) {
	if v, ok := c.scm.(overridesShacFileDirs); ok {
		return v.shacFileDirs(ctx, basename)
	}
	return shacFileDirsFromAllFiles(ctx, c, basename)
}

const (
	// gitModeSymlink is the object mode string that git uses to represent a
	// symlink.
	gitModeSymlink = "120000"
	// gitModeSubmodule is the object mode string that git uses to represent a
	// submodule.
	gitModeSubmodule = "160000"
)

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
func (g *gitCheckout) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	if g.returnAll {
		return g.allFiles(ctx, filter)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	modified := g.affectedTrackedFiles(ctx, filter)
	if g.err != nil {
		return nil, g.err
	}

	// Untracked files are always considered affected (as long as they're not
	// ignored).
	untracked, err := g.untrackedFiles(ctx, filter)
	if err != nil {
		return nil, err
	}
	modified = append(modified, untracked...)

	sort.Slice(modified, func(i, j int) bool { return modified[i].rootedpath() < modified[j].rootedpath() })
	return modified, g.err
}

func (g *gitCheckout) untrackedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	var modified []file
	o := g.run(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	var items []string
	if len(o) > 0 {
		items = strings.Split(o[:len(o)-1], "\x00")
	}
	for _, path := range items {
		absPath := filepath.Join(g.checkoutRoot, path)
		fi, err := os.Lstat(absPath)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}

		var gitMode string
		if fi.Mode()&os.ModeSymlink != 0 {
			gitMode = gitModeSymlink
		}
		inc, err := g.shouldIncludeFile(filter, path, "A", gitMode)
		if err != nil {
			return nil, err
		}
		if !inc {
			continue
		}

		modified = append(modified, &fileImpl{a: "A", path: filepath.ToSlash(path)})
	}
	return modified, nil
}

func (g *gitCheckout) shouldIncludeFile(filter fileFilter, path string, action string, gitMode string) (bool, error) {
	if gitMode == gitModeSubmodule {
		return false, nil
	}
	if action == "D" && !filter.includeDeleted {
		return false, nil
	}
	if gitMode == gitModeSymlink {
		if !filter.includeSymlinks {
			return false, nil
		}
		// For deleted files, we can't check if it was a symlink to a directory.
		// We'll include it, assuming it was a symlink to a file.
		if action != "D" {
			fi, err := os.Stat(filepath.Join(g.checkoutRoot, path))
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil // Dangling symlink
			} else if err != nil {
				return false, err
			}
			if fi.IsDir() {
				return false, nil
			}
		}
	}
	return true, nil
}

func (g *gitCheckout) affectedTrackedFiles(ctx context.Context, filter fileFilter) []file {
	var modified []file
	// Each line has a variable number of NUL character, so process one at a time.
	for o := g.run(ctx, "diff", "--raw", "-z", "-C", g.upstream.hash); len(o) != 0; {
		var action, path string
		var dstMode string
		var srcMode string
		if i := strings.IndexByte(o, 0); i != -1 {
			meta := o[:i]
			o = o[i+1:]

			parts := strings.Fields(meta)
			if len(parts) < 5 {
				g.err = fmt.Errorf("invalid git diff --raw output: %q", meta)
				break
			}
			srcMode = parts[0][1:] // Remove leading ':'
			dstMode = parts[1]
			action = parts[4][:1]

			if i = strings.IndexByte(o, 0); i != -1 {
				path = o[:i]
				o = o[i+1:]

				if action == "C" || action == "R" {
					if i = strings.IndexByte(o, 0); i != -1 {
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
				g.err = fmt.Errorf("missing trailing NUL character from git diff --raw -z -C %s", g.upstream.hash)
			}
			break
		}

		var gitMode string
		if action == "D" {
			gitMode = srcMode
		} else {
			gitMode = dstMode
		}

		inc, err := g.shouldIncludeFile(filter, path, action, gitMode)
		if err != nil {
			g.err = err
			break
		}
		if inc {
			modified = append(modified, &fileImpl{a: action, path: filepath.ToSlash(path)})
		}
	}
	return modified
}

// allFiles returns all the files in this checkout.
//
// The entries are lazy loaded and cached.
func (g *gitCheckout) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Paths are returned in POSIX style even on Windows.
	// TODO(maruel): Extract more information.
	o := g.run(ctx, "ls-files", "-z", "--stage", "--cached", "--others", "--exclude-standard")
	if g.err != nil {
		// If an error occurred on this command or an earlier one, then the
		// ls-files output may not be parseable and we should exit early.
		return nil, g.err
	}
	// Always include deleted files here so they are added to
	// affectedFileActions. This allows allFiles to correctly filter out
	// deleted tracked files without needing to call os.Stat later.
	affected := g.affectedTrackedFiles(ctx, fileFilter{includeDeleted: true, includeSymlinks: filter.includeSymlinks})
	if g.err != nil {
		return nil, g.err
	}
	affectedFileActions := make(map[string]string, len(affected))
	for _, f := range affected {
		affectedFileActions[f.rootedpath()] = f.action()
	}
	items := strings.Split(o[:len(o)-1], "\x00")
	all := make([]file, 0, len(items))
	for _, item := range items {
		if meta, path, ok := strings.Cut(item, "\t"); ok {
			// Tracked file.
			parts := strings.Fields(meta)
			gitMode := parts[0]
			p := filepath.ToSlash(path)
			action := affectedFileActions[p]
			inc, err := g.shouldIncludeFile(filter, path, action, gitMode)
			if err != nil {
				return nil, err
			}
			if inc {
				all = append(all, &fileImpl{a: action, path: p})
			}
		} else {
			// Untracked file.
			path := item
			absPath := filepath.Join(g.checkoutRoot, path)
			fi, err := os.Lstat(absPath)
			if errors.Is(err, fs.ErrNotExist) {
				// This may happen if the file is a dangling symlink or if
				// another process deleted it between the `git ls-files` call
				// and the os.Stat call. In either case we can safely ignore it.
				continue
			} else if err != nil {
				return nil, err
			}

			var gitMode string
			if fi.Mode()&os.ModeSymlink != 0 {
				gitMode = gitModeSymlink
			}
			p := filepath.ToSlash(path)
			action := affectedFileActions[p]

			inc, err := g.shouldIncludeFile(filter, path, action, gitMode)
			if err != nil {
				return nil, err
			}
			if inc {
				all = append(all, &fileImpl{a: action, path: p})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].rootedpath() < all[j].rootedpath() })
	return all, g.err
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
	res := make([]starlark.Value, 0, len(items))
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
			res = append(res, starlark.Tuple{starlark.MakeInt(curr), starlark.String(l[1:])})
			curr++
		} else if !strings.HasPrefix(l, "-") && l != "\\ No newline at end of file" && l != "" {
			panic(fmt.Sprintf("unexpected line %q", l))
		}
	}
	return starlark.Tuple(res), nil
}

func (g *gitCheckout) commits(ctx context.Context) ([]scmCommit, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err != nil {
		return nil, g.err
	}
	o := g.run(ctx, "log", "--format=%H%n%B", "-z", g.upstream.hash+".."+g.head.hash)
	if g.err != nil {
		return nil, g.err
	}
	if o == "" {
		return nil, nil
	}
	parts := strings.Split(o, "\x00")
	var commits []scmCommit
	for _, p := range parts {
		if p == "" {
			continue
		}
		hash, message, ok := strings.Cut(p, "\n")
		if !ok {
			continue
		}
		commits = append(commits, scmCommit{hash: hash, message: message})
	}
	return commits, nil
}

// Generic support.

type rawTreeFile struct {
	path      string
	isSymlink bool
}

type rawTree struct {
	root string

	mu  sync.Mutex
	all []rawTreeFile
}

func (r *rawTree) affectedFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	return r.allFiles(ctx, filter)
}

// allFiles returns all files in this directory tree.
//
// The includeDeleted argument is ignored as only files that exist on disk are
// included.
func (r *rawTree) allFiles(ctx context.Context, filter fileFilter) ([]file, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	if r.all == nil {
		l := len(r.root) + 1
		err = filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err2 error) error {
			if err2 == nil && !d.IsDir() {
				isSymlink := d.Type()&fs.ModeSymlink != 0
				if isSymlink {
					fi, statErr := os.Stat(path)
					if statErr != nil {
						return nil // Skip dangling symlinks.
					}
					if fi.IsDir() {
						return nil // Skip symlinks to directories.
					}
				}
				r.all = append(r.all, rawTreeFile{path: filepath.ToSlash(path[l:]), isSymlink: isSymlink})
			}
			return nil
		})
		sort.Slice(r.all, func(i, j int) bool { return r.all[i].path < r.all[j].path })
	}
	var res []file
	for _, e := range r.all {
		if e.isSymlink && !filter.includeSymlinks {
			continue
		}
		res = append(res, &fileImpl{path: e.path})
	}
	return res, err
}

func (r *rawTree) newLines(ctx context.Context, f file) (starlark.Value, error) {
	return newLinesWhole(r.root, f.rootedpath())
}

func (r *rawTree) commits(ctx context.Context) ([]scmCommit, error) {
	return nil, nil
}

// Starlark adapter code.

// ctxScmAffectedFiles implements native function ctx.scm.affected_files().
//
// It returns a dictionary.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxScmAffectedFiles(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argincludeDeleted starlark.Bool
	var argincludeSymlinks starlark.Bool
	var argglob starlark.Value
	if err := starlark.UnpackArgs(name, args, kwargs,
		"include_deleted?", &argincludeDeleted,
		"include_symlinks?", &argincludeSymlinks,
		"glob??", &argglob,
	); err != nil {
		return nil, err
	}
	files, err := s.scm.affectedFiles(ctx, fileFilter{
		includeDeleted:  bool(argincludeDeleted),
		includeSymlinks: bool(argincludeSymlinks),
	})
	if err != nil {
		return nil, err
	}
	files, err = filterFilesByGlob(files, argglob)
	if err != nil {
		return nil, err
	}
	return ctxScmFilesReturnValue(files), nil
}

// ctxScmAllFiles implements native function ctx.scm.all_files().
//
// It returns a dictionary.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxScmAllFiles(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argincludeDeleted starlark.Bool
	var argincludeSymlinks starlark.Bool
	var argglob starlark.Value
	if err := starlark.UnpackArgs(name, args, kwargs,
		"include_deleted?", &argincludeDeleted,
		"include_symlinks?", &argincludeSymlinks,
		"glob??", &argglob,
	); err != nil {
		return nil, err
	}
	files, err := s.scm.allFiles(ctx, fileFilter{
		includeDeleted:  bool(argincludeDeleted),
		includeSymlinks: bool(argincludeSymlinks),
	})
	if err != nil {
		return nil, err
	}
	files, err = filterFilesByGlob(files, argglob)
	if err != nil {
		return nil, err
	}
	return ctxScmFilesReturnValue(files), nil
}

// ctxScmCommits implements native function ctx.scm.commits().
//
// It returns a tuple of structs.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxScmCommits(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
		return nil, err
	}
	commits, err := s.scm.commits(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]starlark.Value, 0, len(commits))
	for _, c := range commits {
		res = append(res, c.getMetadata())
	}
	return starlark.Tuple(res), nil
}

// ctxScmFilesReturnValue converts a list of files into a starlark.Dict to
// return from the ctx.scm.all_files() and ctx.scm.affected_files() functions.
func ctxScmFilesReturnValue(files []file) starlark.Value {
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
	return newLinesWholeBytes(b)
}

func newLinesWholeBytes(b []byte) (starlark.Value, error) {
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
	// #nosec G103
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func filterFilesByGlob(files []file, glob starlark.Value) ([]file, error) {
	if glob == nil || glob == starlark.None {
		return files, nil
	}
	var patterns []string
	switch v := glob.(type) {
	case starlark.String:
		patterns = []string{string(v)}
	case starlark.Sequence:
		patterns = sequenceToStrings(v)
		if patterns == nil {
			return nil, fmt.Errorf("\"glob\" must be string or sequence of strings")
		}
	default:
		return nil, fmt.Errorf("\"glob\" must be string or sequence of strings")
	}

	// If all patterns are negative, we need to prepend "**" because in
	// gitignore syntax, negative patterns only take effect if a file was also
	// matched by a positive pattern. Otherwise no files would be matched, which
	// is probably not what the user intended.
	allNegative := true
	for _, p := range patterns {
		if !strings.HasPrefix(p, "!") {
			allNegative = false
			break
		}
	}
	var gitPatterns []gitignore.Pattern
	if allNegative && len(patterns) > 0 {
		gitPatterns = append(gitPatterns, gitignore.ParsePattern("**", nil))
	}

	for _, p := range patterns {
		if p == "" {
			return nil, fmt.Errorf("\"glob\" pattern cannot be empty")
		}
		gitPatterns = append(gitPatterns, gitignore.ParsePattern(p, nil))
	}
	matcher := gitignore.NewMatcher(gitPatterns)

	var out []file
	for _, f := range files {
		if matcher.Match(strings.Split(f.rootedpath(), "/"), false) {
			out = append(out, f)
		}
	}
	return out, nil
}
