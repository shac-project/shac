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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/sumdb/dirhash"
)

func TestFSToDigest_Reproducible(t *testing.T) {
	t.Parallel()

	// Reuse a simple Go project that is in Go Proxy
	// (https://proxy.golang.org/).  This ensures the algorithm matches the
	// expected value.
	//
	// Created with:
	//  git clone https://github.com/maruel/ut -b v1.0.0
	//  cd ut
	//  cp *.go LICENSE README.md .travis.yml .git/config ../internal/engine/testdata/ut
	//
	// Ideally we could use a module already included the vendor directory, but
	// Go's vendoring process strips out test files and .git files even though
	// they are taken into account by the mod hash computation. We need to take
	// an alternate approach that doesn't strip out those files, so bypass the
	// normal vendoring process.
	srcDir := filepath.Join("testdata", "ut")

	root := t.TempDir()

	copyTree(t, root, srcDir, map[string]string{
		// Git doesn't allow committing the .git directory, but .git/config
		// needs to be considered in the hash computation, so we commit it to
		// `config` instead of `.git/config` and then copy it into the right
		// place.
		"config": ".git/config",
	})

	const prefix = "github.com/maruel/ut@v1.0.0"
	// Retrieved from an empty directory:
	//   go mod init main
	//   go get github.com/maruel/ut@v1.0.0
	//   grep maruel/ut go.sum
	const knownHash = "h1:Tg5f5waOijrohsOwnMlr1bZmv+wHEbuMEacNBE8kQ7k="

	// Test our code to ensure it matches the go proxy.
	if d, err := FSToDigest(os.DirFS(root), prefix); err != nil {
		t.Fatal(err)
	} else if d != knownHash {
		t.Errorf("expected %s, got %s", knownHash, d)
	}

	// Now test with the standard enumerating code.
	// Remove .git/config here, since this function doesn't filter it out.
	if err := os.Remove(filepath.Join(root, ".git", "config")); err != nil {
		t.Fatal(err)
	}
	if d, err := dirhash.HashDir(root, prefix, dirhash.Hash1); err != nil {
		t.Fatal(err)
	} else if d != knownHash {
		t.Errorf("expected %s, got %s", knownHash, d)
	}
}

func copyTree(t *testing.T, dstDir, srcDir string, renamings map[string]string) {
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if newName, ok := renamings[rel]; ok {
			rel = newName
		}
		dest := filepath.Join(dstDir, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dest, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFSToDigest_Fail(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if d, err := FSToDigest(os.DirFS(root), ""); d != "" {
		t.Fatal(d)
	} else if err == nil || err.Error() != "prefix is required" {
		t.Fatal(err)
	}
	want := "stat .: no such file or directory"
	if runtime.GOOS == "windows" {
		want = "CreateFile .: The system cannot find the file specified."
	}
	if d, err := FSToDigest(os.DirFS(filepath.Join(root, "inexistant")), "prefix"); d != "" {
		t.Fatal(d)
	} else if err == nil || err.Error() != want {
		t.Fatal(err)
	}
}

func TestPackageManager(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var cmds []string
	p := PackageManager{
		root: root,
		gitCommand: func(ctx context.Context, d string, args ...string) error {
			if !strings.HasPrefix(d, root) {
				t.Errorf("%s doesn't have prefix %s", d, root)
			}
			if len(args) >= 2 && args[0] == "clone" {
				// Kind of a hack to create the directory on git clone when mocking.
				if err := os.Mkdir(filepath.Join(d, args[2]), 0o700); err != nil && errors.Is(err, fs.ErrNotExist) {
					t.Error(err)
				}
			}
			s := fmt.Sprintf("%s %s", strings.ReplaceAll(d[len(root):], "\\", "/"), strings.Join(args, " "))
			cmds = append(cmds, s)
			return nil
		},
		pkgConcurrency: 1,
	}
	doc := Document{
		Requirements: &Requirements{
			Direct: []*Dependency{
				{
					Url:     "example.com/bar",
					Version: "version",
				},
				{
					Url:     "github.com/shac-project/shac",
					Version: "pull/1/head",
				},
			},
			Indirect: []*Dependency{
				{
					Url:     "example.com/gerrit",
					Version: "refs/changes/45/12345/12",
				},
			},
		},
		Sum: &Sum{
			Known: []*Known{
				{
					Url: "example.com/bar",
					Seen: []*VersionDigest{
						{
							Version: "version",
							Digest:  "h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
						},
					},
				},
				{
					Url: "example.com/gerrit",
					Seen: []*VersionDigest{
						{
							Version: "refs/changes/45/12345/12",
							Digest:  "h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
						},
					},
				},
				{
					Url: "github.com/shac-project/shac",
					Seen: []*VersionDigest{
						{
							Version: "pull/1/head",
							Digest:  "h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
						},
					},
				},
			},
		},
	}
	_, err := p.RetrievePackages(context.Background(), root, &doc)
	if err != nil {
		t.Error(err)
	}
	want := []string{
		"/example.com clone https://example.com/bar bar@version",
		"/example.com/bar@version checkout version",
		"/github.com/shac-project/shac fetch https://github.com/shac-project/shac pull/1/head",
		"/github.com/shac-project clone https://github.com/shac-project/shac shac",
		"/github.com/shac-project/shac checkout FETCH_HEAD",
		"/example.com/gerrit fetch https://example.com/gerrit refs/changes/45/12345/12",
		"/example.com clone https://example.com/gerrit gerrit",
		"/example.com/gerrit checkout FETCH_HEAD",
	}
	if diff := cmp.Diff(want, cmds); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func TestPackageManager_Err(t *testing.T) {
	doc := Document{}
	d := t.TempDir()
	p := NewPackageManager("foo")
	if _, err := p.RetrievePackages(context.Background(), d, &doc); err == nil {
		t.Fatal("expected error; path is not absolute")
	}
	p = NewPackageManager(filepath.Join(d, "non_existent"))
	if _, err := p.RetrievePackages(context.Background(), d, &doc); err == nil {
		t.Fatal("expected error")
	}
	p = NewPackageManager(d)
	if _, err := p.RetrievePackages(context.Background(), "foo", &doc); err == nil {
		t.Fatal("expected error; path is not absolute")
	}
	if _, err := p.RetrievePackages(context.Background(), filepath.Join(d, "non_existent"), &doc); err == nil {
		t.Fatal("expected error")
	}
}
