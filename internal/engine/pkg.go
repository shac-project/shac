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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/sync/errgroup"
)

// FSToDigest hash the content of a directory tree and return the hash.
//
// Use a similar hashing mechanism than Go Modules. See implementation at
// https://github.com/golang/mod/blob/v0.10.0/sumdb/dirhash/hash.go or a
// more recent version.
//
// The directories at root starting with a dot "." are ignored. This includes
// .git, .github, .vscode, etc. As such the digest may differ a bit from Go.
// This may be revisited.
func FSToDigest(f fs.FS, prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("prefix is required")
	}
	var files []string
	err := fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
		} else if d.IsDir() {
			if p != "." && p[0] == '.' {
				err = fs.SkipDir
			}
		} else {
			// It's not strictly speaking a real path.
			files = append(files, prefix+"/"+strings.ReplaceAll(p, "\\", "/"))
		}
		return err
	})
	if err != nil {
		return "", err
	}
	l := len(prefix) + 1
	return dirhash.Hash1(files, func(name string) (io.ReadCloser, error) {
		f, err := f.Open(name[l:])
		if err != nil {
			return nil, fmt.Errorf("couldn't open %s (prefix: %s): %w", name, prefix, err)
		}
		return f, nil
	})
}

// PackageManager manages dependencies, both fetching and verifying the hashes.
type PackageManager struct {
	// Root is the location where dependencies are fetched.
	//
	// It is valid for this path to be a scratch space.
	Root string
}

// RetrievePackages retrieve all the packages in parallel, up to 8 threads.
func (p *PackageManager) RetrievePackages(ctx context.Context, root string, doc *Document) (map[string]fs.FS, error) {
	if !filepath.IsAbs(p.Root) {
		return nil, fmt.Errorf("path %s is not absolute", p.Root)
	}
	if err := isDir(p.Root); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("path %s is not absolute", root)
	}
	if err := isDir(root); err != nil {
		return nil, err
	}
	mu := sync.Mutex{}
	packages := map[string]fs.FS{"__main__": os.DirFS(root)}
	var depslists [][]*Dependency
	if doc.Requirements != nil {
		depslists = [][]*Dependency{doc.Requirements.Direct, doc.Requirements.Indirect}
	}

	if doc.VendorPath != "" {
		// Use the vendored versions.
		vendorRoot := filepath.Join(root, doc.VendorPath)
		for _, deps := range depslists {
			// Do it serially for now, decided if parallelism helps performance later.
			for _, d := range deps {
				// url is believed to be vetted at this point.
				dir := filepath.Join(vendorRoot, d.Url)
				if err := isDir(dir); err != nil {
					return packages, fmt.Errorf("vendored %w", err)
				}
				f, err := p.verifyDir(dir, d.Url, d.Version, doc.Sum.Digest(d.Url, d.Version))
				if err != nil {
					return packages, fmt.Errorf("%s couldn't be fetched: %w", d.Url, err)
				}
				packages[d.Url] = f
				if d.Alias != "" {
					packages[d.Alias] = f
				}
			}
		}
		return packages, nil
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(pkgConcurrency)
	for _, deps := range depslists {
		for _, d := range deps {
			d := d
			eg.Go(func() error {
				f, err := p.ensureGitPkg(ctx, d.Url, d.Version, doc.Sum.Digest(d.Url, d.Version))
				if err != nil {
					return fmt.Errorf("%s couldn't be fetched: %w", d.Url, err)
				}
				mu.Lock()
				packages[d.Url] = f
				if d.Alias != "" {
					packages[d.Alias] = f
				}
				mu.Unlock()
				return nil
			})
		}
	}
	err := eg.Wait()
	return packages, err
}

// ensureGitPkg returns a fs.FS for the dependency, assuming a git remote.
//
// It is invalid to retrieve the same dependency at multiple versions during a
// single session.
func (p *PackageManager) ensureGitPkg(ctx context.Context, url, version string, digest string) (fs.FS, error) {
	fullURL, err := cleanURL(url)
	if err != nil {
		return nil, err
	}

	depdir := filepath.Join(p.Root, url)
	if ok, _ := regexp.MatchString("^refs/changes/\\d{1,2}/\\d{1,11}/\\d{1,3}$", version); ok {
		// Explicitly enable support using a pending Gerrit CL.
		if err = gitCommand(ctx, depdir, "fetch", fullURL, version); err != nil {
			return nil, err
		}
		version = "FETCH_HEAD"
	} else if ok, _ := regexp.MatchString("^pull/\\d+/head$", version); ok {
		// Explicitly enable support using a pending GitHub PR.
		if err = gitCommand(ctx, depdir, "fetch", fullURL, version); err != nil {
			return nil, err
		}
		version = "FETCH_HEAD"
	} else {
		// Use a format similar to Go modules cache.
		v := ""
		if v, err = module.EscapeVersion(version); err != nil {
			return nil, err
		}
		depdir += "@" + v
	}

	parentdir := filepath.Dir(depdir)
	if err = os.MkdirAll(parentdir, 0o777); err != nil {
		return nil, err
	}
	if err = gitCommand(ctx, parentdir, "clone", fullURL, filepath.Base(depdir)); err != nil {
		return nil, err
	}
	if err = gitCommand(ctx, depdir, "checkout", version); err != nil {
		return nil, err
	}
	return p.verifyDir(depdir, url, version, digest)
}

// verifyDir returns a fs.FS that maps to path `d`, after having confirmed
// that the content matches the expected digest, if set.
func (p *PackageManager) verifyDir(d, url, version, digest string) (fs.FS, error) {
	f := os.DirFS(d)
	// Verify the hash.
	if digest != "" {
		got, err := FSToDigest(f, url+"@"+version)
		if err != nil {
			return nil, fmt.Errorf("hashing failed: %w", err)
		}
		if got != digest {
			return nil, fmt.Errorf("mismatched digest, got %s, expected %s", got, digest)
		}
	}
	return f, nil
}

// gitReal runs git for packages fetching.
//
// Unlike runtime_ctx_scm.go operations, this one doesn't skip the user's
// configuration. The rationale is that some dependencies may require url
// rewrite or authentication.
func gitReal(ctx context.Context, d string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = d
	if o, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, o)
	}
	return nil
}

// isDir returns an error if the path is not a directory.
func isDir(d string) error {
	s, err := os.Stat(d)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("path %s is missing", d)
	}
	if err != nil {
		return err
	}
	if !s.IsDir() {
		return fmt.Errorf("path %s is not a directory", d)
	}
	return nil
}

// Overridden in unit testing.
var gitCommand = gitReal
var pkgConcurrency = 8
