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
	Root string

	mu    sync.Mutex
	index int
}

// RetrievePackages retrieve all the packages in parallel, up to 8 threads.
func (p *PackageManager) RetrievePackages(ctx context.Context, root string, doc *Document) (map[string]fs.FS, error) {
	mu := sync.Mutex{}
	packages := map[string]fs.FS{"__main__": os.DirFS(root)}
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(8)
	if doc.Requirements != nil {
		for _, deps := range [...][]*Dependency{doc.Requirements.Direct, doc.Requirements.Indirect} {
			for _, d := range deps {
				d := d
				eg.Go(func() error {
					f, err := p.pkg(ctx, d.Url, d.Version, doc.Sum.Digest(d.Url, d.Version))
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
	}
	err := eg.Wait()
	return packages, err
}

// pkg returns a fs.FS for the dependency.
func (p *PackageManager) pkg(ctx context.Context, urlOrig, version string, digest string) (fs.FS, error) {
	p.mu.Lock()
	i := p.index
	p.index++
	p.mu.Unlock()

	url, err := cleanURL(urlOrig)
	if err != nil {
		return nil, err
	}
	subdir := fmt.Sprintf("dep%d", i)
	if err := gitCommand(ctx, p.Root, "clone", url, subdir); err != nil {
		return nil, err
	}
	d := filepath.Join(p.Root, subdir)
	if ok, _ := regexp.MatchString("^refs/changes/\\d{1,2}/\\d{1,11}/\\d{1,3}$", version); ok {
		// Explicitly enable support using a pending Gerrit CL.
		if err := gitCommand(ctx, d, "fetch", url, version); err != nil {
			return nil, err
		}
		version = "FETCH_HEAD"
	} else if ok, _ := regexp.MatchString("^pull/\\d+/head$", version); ok {
		// Explicitly enable support using a pending GitHub PR.
		if err := gitCommand(ctx, d, "fetch", url, version); err != nil {
			return nil, err
		}
		version = "FETCH_HEAD"
	}
	if err := gitCommand(ctx, d, "checkout", version); err != nil {
		return nil, err
	}

	f := os.DirFS(d)
	// Verify the hash.
	if digest != "" {
		got, err := FSToDigest(f, urlOrig+"@"+version)
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

// Overridden in unit testing.
var gitCommand = gitReal
