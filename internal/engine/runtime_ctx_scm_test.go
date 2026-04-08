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
	"testing"

	"github.com/google/go-cmp/cmp"
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

	ctx := context.Background()

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
