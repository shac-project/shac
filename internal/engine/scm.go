// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"go.chromium.org/luci/starlark/interpreter"
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

type file struct {
	path   string
	action string
}

// scmCheckout is the generic interface for version controlled sources.
type scmCheckout interface {
	affectedFiles(ctx context.Context) ([]file, error)
	allFiles(ctx context.Context) ([]file, error)
}

// Git support.

func getSCM(ctx context.Context, root string) scmCheckout {
	g := &gitCheckout{}
	err := g.init(ctx, root)
	if err == nil {
		return g
	}
	log.Printf("git not detected: %s", err)
	// TODO(maruel): Add the scm of your choice.
	return &rawTree{root: root}
}

// gitCheckout represents a git checkout.
type gitCheckout struct {
	head     commitRef
	upstream commitRef
	root     string // root path may differ from the check's root!
	env      []string

	mu       sync.Mutex
	modified []file // modified files in this checkout
	all      []file // all files in the repo.
	err      error  // save error.
}

func (g *gitCheckout) init(ctx context.Context, root string) error {
	// Find root.
	g.root = root
	g.root = g.run(ctx, "rev-parse", "--show-toplevel")
	g.head.hash = g.run(ctx, "rev-parse", "HEAD")
	g.head.ref = g.run(ctx, "rev-parse", "--abbrev-ref=strict", "--symbolic-full-name", "HEAD")
	if g.err != nil {
		// Not worth continuing.
		return g.err
	}
	// Determine pristine status but ignoring untracked files. We do not
	// distinguish between indexed or not.
	isPristine := "" == g.run(ctx, "status", "--porcelain", "--untracked-files=no")
	g.upstream.hash = g.run(ctx, "rev-parse", "@{u}")
	g.upstream.ref = g.run(ctx, "rev-parse", "--abbrev-ref=strict", "--symbolic-full-name", "@{u}")
	if g.err != nil {
		const noUpstream = "no upstream configured for branch"
		const noBranch = "HEAD does not point to a branch"
		if s := g.err.Error(); strings.Contains(s, noUpstream) || strings.Contains(s, noBranch) {
			// If @{u} is undefined, silently default to use HEAD~1 if pristine, HEAD otherwise.
			g.err = nil
			if isPristine {
				// If HEAD~1 doesn't exist, this will fail.
				g.upstream.ref = "HEAD~1"
			} else {
				g.upstream.ref = "HEAD"
			}
		}
	}
	return g.err
}

// run runs a git command in the check. After init() is called, the mu lock is
// expected to be held.
func (g *gitCheckout) run(ctx context.Context, args ...string) string {
	if g.err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.root
	if g.env == nil {
		// First is for git version before 2.32, the rest are to skip the user and system config.
		g.env = append(os.Environ(), "GIT_CONFIG_NOGLOBAL=true", "GIT_CONFIG_GLOBAL=", "GIT_CONFIG_SYSTEM=")
	}
	cmd.Env = g.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.err = fmt.Errorf("error running git %s: %s", strings.Join(args, " "), out)
	}
	return strings.TrimSpace(string(out))
}

// affectedFiles returns the modified files on this checkout.
//
// The entries are lazy loaded and cached.
func (g *gitCheckout) affectedFiles(ctx context.Context) ([]file, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.modified == nil {
		// TODO(maruel): Using --find-copies-harder would be too slow for large
		// repositories. It'd be nice to autodetect?
		if o := g.run(ctx, "diff", "--name-status", "-z", "-C", g.upstream.ref); len(o) != 0 {
			// This code keeps a hold pointers on the original buffer. It's not a big deal.
			items := strings.Split(o[:len(o)-1], "\x00")
			g.modified = make([]file, len(items)/2)
			for i := 0; i < len(items); i += 2 {
				g.modified[i/2].action = items[i]
				g.modified[i/2].path = items[i+1]
			}
			sort.Slice(g.modified, func(i, j int) bool { return g.modified[i].path < g.modified[j].path })
		} else {
			g.modified = []file{}
		}
	}
	return g.modified, g.err
}

// allFiles returns all the files in this checkout.
//
// The entries are lazy loaded and cached.
func (g *gitCheckout) allFiles(ctx context.Context) ([]file, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.all == nil {
		// TODO(maruel): Extract more information.
		if o := g.run(ctx, "ls-files", "-z"); len(o) != 0 {
			items := strings.Split(o[:len(o)-1], "\x00")
			g.all = make([]file, 0, len(items))
			for i := 0; i < len(items); i++ {
				g.all = append(g.all, file{path: items[i]})
			}
			sort.Slice(g.all, func(i, j int) bool { return g.all[i].path < g.all[j].path })
		} else {
			g.all = []file{}
		}
	}
	return g.all, g.err
}

// Generic support.

type rawTree struct {
	root string

	mu  sync.Mutex
	all []file
}

func (r *rawTree) affectedFiles(ctx context.Context) ([]file, error) {
	return r.allFiles(ctx)
}

// allFiles returns all files in this directory tree.
func (r *rawTree) allFiles(ctx context.Context) ([]file, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.all == nil {
		l := len(r.root) + 1
		filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
			if err == nil {
				if !d.IsDir() {
					r.all = append(r.all, file{path: path[l:]})
				}
			}
			return nil
		})
	}
	return r.all, nil
}

// Starlark adapter code.

func scmFilesCommon(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple, all bool) (starlark.Value, error) {
	if len(args) > 0 {
		return starlark.None, fmt.Errorf("%s: unexpected arguments", fn.Name())
	}
	if len(kwargs) > 0 {
		return starlark.None, fmt.Errorf("%s: unexpected keyword arguments", fn.Name())
	}
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	var files []file
	var err error
	if s.inputs.allFiles || all {
		files, err = s.scm.allFiles(ctx)
	} else {
		files, err = s.scm.affectedFiles(ctx)
	}
	if err != nil {
		return starlark.None, err
	}
	// files is guaranteed to be sorted.
	out := starlark.NewDict(len(files))
	for _, f := range files {
		out.SetKey(starlark.String(f.path), toValue("file", starlark.StringDict{
			// TODO(maruel): Add new_lines() function to get the new lines from this file.
			"action": starlark.String(f.action),
		}))
	}
	return out, nil
}

// scmAffectedFiles implements native function shac.scm.affected_files().
//
// It returns a dictionary.
func scmAffectedFiles(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return scmFilesCommon(th, fn, args, kwargs, false)
}

// scmAllFiles implements native function shac.scm.all_files().
//
// It returns a dictionary.
func scmAllFiles(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return scmFilesCommon(th, fn, args, kwargs, true)
}
