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
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/sumdb/dirhash"
)

func TestFSToDigest_Reproducible(t *testing.T) {
	t.Parallel()
	// Reuse a simple Go project that is in Go Proxy (https://proxy.golang.org/).
	// This ensures the algorithm matches the expected value.
	// Intentionally unzip to disk so we don't use funky in-memory file system
	// for unit test, only to have it fail in practice.
	root := t.TempDir()
	// Created with:
	//  git clone https://github.com/maruel/ut -b v1.0.0
	//  cd ut
	//  zip ut_v1.0.0.zip *.go LICENSE README.md .travis.yml .git/config
	unzip(t, root, filepath.Join("testdata", "ut_v1.0.0.zip"))
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
	mu := sync.Mutex{}
	var cmds []string
	old := gitCommand
	t.Cleanup(func() {
		gitCommand = old
	})
	gitCommand = func(ctx context.Context, d string, args ...string) error {
		if !strings.HasPrefix(d, root) {
			t.Errorf("%s doesn't have prefix %s", d, root)
		}
		if d != root {
			if err := os.Mkdir(d, 0o700); err != nil && errors.Is(err, fs.ErrNotExist) {
				t.Error(err)
			}
		}
		// Simplify expectations, otherwise the output is non-deterministic.
		for i := range args {
			if strings.HasPrefix(args[i], "dep") {
				args[i] = "dep"
			}
		}
		d = d[len(root):]
		if d != "" {
			d = d[:len(d)-1]
		}
		mu.Lock()
		cmds = append(cmds, fmt.Sprintf("%s %s", d, strings.Join(args, " ")))
		mu.Unlock()
		return nil
	}
	p := PackageManager{Root: root}
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
	// There's a race condition in which of the dependency will be assigned dep0.
	want := []string{
		" clone https://example.com/bar dep",
		" clone https://example.com/gerrit dep",
		" clone https://github.com/shac-project/shac dep",
		string(os.PathSeparator) + "dep checkout FETCH_HEAD",
		string(os.PathSeparator) + "dep checkout FETCH_HEAD",
		string(os.PathSeparator) + "dep checkout version",
		string(os.PathSeparator) + "dep fetch https://example.com/gerrit refs/changes/45/12345/12",
		string(os.PathSeparator) + "dep fetch https://github.com/shac-project/shac pull/1/head",
	}
	sort.Strings(cmds)
	if diff := cmp.Diff(want, cmds); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func unzip(t *testing.T, dst, in string) {
	r, err := zip.OpenReader(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Error(err)
		}
		if err = rc.Close(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(f.Name, "/") {
			if err = os.MkdirAll(filepath.Join(dst, path.Dir(f.Name)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err = os.WriteFile(filepath.Join(dst, f.Name), data, f.Mode()); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
