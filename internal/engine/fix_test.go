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
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFix(t *testing.T) {
	t.Parallel()

	originalLines := []string{
		"These are",
		"the contents",
		"of the file",
		"that may be modified",
	}

	// TODO(olivernewman): Checks that emit findings in different files.
	data := []struct {
		name  string
		want  []string
		level Level
	}{
		{
			name: "delete_lines.star",
			want: []string{
				"These are",
				"that may be modified",
			},
		},
		{
			name: "ignored_findings.star",
			want: originalLines,
		},
		{
			name: "multiple_replacements_one_file.star",
			want: []string{
				"<REPL1>",
				"the contents",
				"<REPL2>",
			},
		},
		{
			name: "replace_entire_file.star",
			want: []string{
				"this text is a replacement",
				"for the entire file",
			},
		},
		{
			name: "replace_entire_file_others_ignored.star",
			want: []string{
				"this text is a replacement",
				"for the entire file",
			},
		},
		{
			name: "replace_one_full_line.star",
			want: []string{
				"These are",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
		},
		{
			name: "replace_partial_line.star",
			want: []string{
				"These are",
				"the contents",
				"of UPDATED file",
				"that may be modified",
			},
		},
		{
			name: "various_level_findings.star",
			want: []string{
				"NOTICE",
				"WARNING",
				"ERROR",
				"that may be modified",
			},
		},
	}
	want := make([]string, len(data))
	for i := range data {
		want[i] = data[i].name
	}
	_, got := enumDir(t, "fix")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			m, err := filepath.Glob(filepath.Join("testdata", "fix", "*"))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, root, "file.txt", strings.Join(originalLines, "\n")+"\n")
			for _, src := range m {
				copyFile(t, root, src)
			}

			o := Options{
				Dir:        root,
				EntryPoint: data[i].name,
				config:     "../config/valid.textproto",
			}
			if err := Fix(context.Background(), &o, true); err != nil {
				t.Fatal(err)
			}
			got := strings.Split(readFile(t, filepath.Join(root, "file.txt")), "\n")
			want := append(data[i].want, "")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("Wrong updated file lines (-want +got):\n%s", diff)
			}
		})
	}
}
