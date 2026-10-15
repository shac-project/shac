// Copyright 2026 The Shac Authors
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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStatImpl(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	type testCase struct {
		name string
		path string
		want string
	}
	testCases := []testCase{
		{name: "file", path: file, want: "stat(is_dir = False, is_file = True)"},
		{name: "dir", path: dir, want: "stat(is_dir = True, is_file = False)"},
		{name: "missing", path: filepath.Join(root, "missing"), want: "None"},
		{name: "parent is a file", path: filepath.Join(file, "child"), want: "None"},
	}
	if runtime.GOOS != "windows" {
		loop := filepath.Join(root, "loop")
		if err := os.Symlink(loop, loop); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(file, link); err != nil {
			t.Fatal(err)
		}
		dangling := filepath.Join(root, "dangling")
		if err := os.Symlink(filepath.Join(root, "missing"), dangling); err != nil {
			t.Fatal(err)
		}
		testCases = append(testCases,
			testCase{name: "symlink loop", path: loop, want: "None"},
			testCase{name: "symlink to file", path: link, want: "stat(is_dir = False, is_file = True)"},
			testCase{name: "dangling symlink", path: dangling, want: "None"},
		)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := statImpl(tc.path)
			if err != nil {
				t.Fatalf("statImpl(%q) failed: %s", tc.path, err)
			}
			if got.String() != tc.want {
				t.Errorf("statImpl(%q) = %s, want %s", tc.path, got, tc.want)
			}
		})
	}
}

func TestStatImplPermissionDenied(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions work differently on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o700)
	})

	_, err := statImpl(filepath.Join(locked, "file.txt"))
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("got error %v, want permission denied", err)
	}
	// The absolute host path would make error messages nondeterministic.
	if strings.Contains(err.Error(), root) {
		t.Errorf("error %q contains host path %q", err, root)
	}
}
