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
		name string
		want []string
	}{
		{
			"delete_lines.star",
			[]string{
				"These are",
				"that may be modified",
			},
		},
		{
			"ignored_findings.star",
			originalLines,
		},
		{
			"multiple_replacements_one_file.star",
			[]string{
				"<REPL1>",
				"the contents",
				"<REPL2>",
			},
		},
		{
			"replace_entire_file.star",
			[]string{
				"this text is a replacement",
				"for the entire file",
			},
		},
		{
			"replace_entire_file_others_ignored.star",
			[]string{
				"this text is a replacement",
				"for the entire file",
			},
		},
		{
			"replace_one_full_line.star",
			[]string{
				"These are",
				"the contents",
				"UPDATED",
				"that may be modified",
			},
		},
		{
			"replace_partial_line.star",
			[]string{
				"These are",
				"the contents",
				"of UPDATED file",
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
				Root:   root,
				main:   data[i].name,
				config: "../config/valid.textproto",
			}
			if err := Fix(context.Background(), &o); err != nil {
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
