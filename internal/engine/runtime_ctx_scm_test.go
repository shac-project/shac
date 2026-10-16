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
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
)

func TestInMemoryFile(t *testing.T) {
	t.Parallel()

	data := []byte("line1\nline2\nline3")
	tf := &fileImpl{path: "foo.txt", a: "A"}
	s := &inMemoryFile{
		data:       data,
		targetFile: tf,
		root:       "/tmp",
	}

	ctx := t.Context()

	t.Run("affectedFiles", func(t *testing.T) {
		files, err := s.affectedFiles(ctx, fileFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0] != tf {
			t.Errorf("got %v, want [%v]", files, tf)
		}
	})

	t.Run("allFiles", func(t *testing.T) {
		files, err := s.allFiles(ctx, fileFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0] != tf {
			t.Errorf("got %v, want [%v]", files, tf)
		}
	})

	t.Run("newLines", func(t *testing.T) {
		got, err := s.newLines(ctx, tf)
		if err != nil {
			t.Fatal(err)
		}
		want := `((1, "line1"), (2, "line2"), (3, "line3"))`
		if got.String() != want {
			t.Errorf("newLines() = %s, want %s", got.String(), want)
		}
	})

	t.Run("newLinesBinary", func(t *testing.T) {
		sBinary := &inMemoryFile{
			data:       []byte("binary\x00data"),
			targetFile: tf,
		}
		got, err := sBinary.newLines(ctx, tf)
		if err != nil {
			t.Fatal(err)
		}
		want := `()`
		if got.String() != want {
			t.Errorf("newLines() = %s, want %s", got.String(), want)
		}
	})
}

func TestGitConfigEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		gitConfig map[string]string
		want      []string
	}{
		{
			"empty config",
			nil,
			[]string{"GIT_CONFIG_COUNT=0"},
		},
		{
			"one variable",
			map[string]string{
				"foo.bar": "baz",
			},
			[]string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=foo.bar",
				"GIT_CONFIG_VALUE_0=baz",
			},
		},
		{
			"multiple variables",
			map[string]string{
				"foo.bar":          "baz",
				"a_variable":       "a_value",
				"another_variable": "another_value",
			},
			[]string{
				"GIT_CONFIG_COUNT=3",
				"GIT_CONFIG_KEY_0=a_variable",
				"GIT_CONFIG_VALUE_0=a_value",
				"GIT_CONFIG_KEY_1=another_variable",
				"GIT_CONFIG_VALUE_1=another_value",
				"GIT_CONFIG_KEY_2=foo.bar",
				"GIT_CONFIG_VALUE_2=baz",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := gitConfigEnv(tt.gitConfig)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("gitConfigEnv() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCtxScmFilesCaching(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	vscm := &virtualSCM{
		files: []*virtualFile{
			newVirtualFile("a.py", "M", "print(1)\n", true),
			newVirtualFile("b.star", "M", "pass\n", false),
			newVirtualFile("deleted.py", "D", "", true),
		},
	}
	s := &shacState{
		scm: vscm,
	}

	callAffected := func(kwargs ...starlark.Tuple) *starlark.Dict {
		t.Helper()
		val, err := ctxScmAffectedFiles(ctx, s, "ctx.scm.affected_files", nil, kwargs)
		if err != nil {
			t.Fatalf("ctxScmAffectedFiles() unexpected error: %s", err)
		}
		d, ok := val.(*starlark.Dict)
		if !ok {
			t.Fatalf("got %T, want *starlark.Dict", val)
		}
		if err := d.SetKey(starlark.String("mutated"), starlark.None); err == nil {
			t.Fatal("expected returned dict to be frozen, but SetKey succeeded")
		}
		return d
	}

	callAll := func(kwargs ...starlark.Tuple) *starlark.Dict {
		t.Helper()
		val, err := ctxScmAllFiles(ctx, s, "ctx.scm.all_files", nil, kwargs)
		if err != nil {
			t.Fatalf("ctxScmAllFiles() unexpected error: %s", err)
		}
		d, ok := val.(*starlark.Dict)
		if !ok {
			t.Fatalf("got %T, want *starlark.Dict", val)
		}
		if err := d.SetKey(starlark.String("mutated"), starlark.None); err == nil {
			t.Fatal("expected returned dict to be frozen, but SetKey succeeded")
		}
		return d
	}

	affected1 := callAffected()
	affected2 := callAffected()
	if affected1 != affected2 {
		t.Errorf("repeated affected_files() calls returned different dict pointers: %p vs %p", affected1, affected2)
	}

	all1 := callAll()
	all2 := callAll()
	if all1 != all2 {
		t.Errorf("repeated all_files() calls returned different dict pointers: %p vs %p", all1, all2)
	}
	if affected1 == all1 {
		t.Error("affected_files() and all_files() returned the same dict pointer")
	}

	// String glob, list glob, and tuple glob with the same pattern should hit
	// the same cache entry.
	globStr := callAffected(starlark.Tuple{starlark.String("glob"), starlark.String("*.py")})
	globList := callAffected(starlark.Tuple{starlark.String("glob"), starlark.NewList([]starlark.Value{starlark.String("*.py")})})
	globTuple := callAffected(starlark.Tuple{starlark.String("glob"), starlark.Tuple{starlark.String("*.py")}})
	if globStr != globList || globStr != globTuple {
		t.Errorf("equivalent glob calls returned different dict pointers: str=%p list=%p tuple=%p", globStr, globList, globTuple)
	}
	if globStr == affected1 {
		t.Error("affected_files(glob=\"*.py\") returned the same dict pointer as affected_files()")
	}

	// Empty sequence glob=[] must not collide with glob=None.
	globEmpty := callAffected(starlark.Tuple{starlark.String("glob"), starlark.NewList(nil)})
	if globEmpty == affected1 {
		t.Error("affected_files(glob=[]) collided with affected_files()")
	}
	if globEmpty.Len() != 0 {
		t.Errorf("affected_files(glob=[]) len = %d, want 0", globEmpty.Len())
	}

	// Different include_deleted values must return distinct cached dicts.
	incDeleted1 := callAffected(starlark.Tuple{starlark.String("include_deleted"), starlark.True})
	incDeleted2 := callAffected(starlark.Tuple{starlark.String("include_deleted"), starlark.True})
	if incDeleted1 != incDeleted2 {
		t.Errorf("repeated affected_files(include_deleted=True) returned different dict pointers: %p vs %p", incDeleted1, incDeleted2)
	}
	if incDeleted1 == affected1 {
		t.Error("affected_files(include_deleted=True) collided with affected_files()")
	}

	// Concurrent callers with identical and distinct globs must be race-free
	// and return the same cached pointer per key.
	sConcurrent := &shacState{
		scm: vscm,
	}
	const workers = 16
	results := make([]*starlark.Dict, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			var kwargs []starlark.Tuple
			if i%2 == 1 {
				kwargs = []starlark.Tuple{{starlark.String("glob"), starlark.String("*.py")}}
			}
			val, err := ctxScmAffectedFiles(ctx, sConcurrent, "ctx.scm.affected_files", nil, kwargs)
			if err != nil {
				t.Errorf("ctxScmAffectedFiles() unexpected error: %s", err)
				return
			}
			d := val.(*starlark.Dict)
			_ = d.Items()
			results[i] = d
		})
	}
	wg.Wait()
	for i := range workers {
		if results[i] != results[i%2] {
			t.Errorf("worker %d got dict %p, want %p", i, results[i], results[i%2])
		}
	}
}
