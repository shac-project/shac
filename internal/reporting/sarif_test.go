// Copyright 2025 The Shac Authors
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

package reporting

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/shac-project/shac/internal/sarif"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestReplacementsForDiff(t *testing.T) {
	a := strings.Split("ABCDEFGHIJLKMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", "")
	for i := range a {
		a[i] += "\n"
	}

	// Copy the original and make some modifications. Modifications are in
	// reverse order so that the line numbers here correspond (with an offset of
	// one) to the line numbers in the expected data.
	b := slices.Clone(a)

	var want []*sarif.Replacement

	// Add a new line at the end without a trailing new line.
	b = append(b, "9")
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine:   53,
			EndLine:     53,
			StartColumn: 1,
			EndColumn:   1,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "9",
		},
	})

	// Replace one line with multiple lines.
	b = slices.Replace(b, 30, 31, "2\n", "3\n", "4\n", "5\n", "6\n")
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 31,
			EndLine:   31,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "2\n3\n4\n5\n6\n",
		},
	})

	// Insert a blank line.
	b = slices.Insert(b, 24, "\n")
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine:   25,
			EndLine:     25,
			StartColumn: 1,
			EndColumn:   1,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "\n",
		},
	})

	// Delete a few elements.
	b = slices.Delete(b, 13, 18)
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 14,
			EndLine:   18,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "",
		},
	})

	// Change a single line.
	b[3] = "1\n"
	want = append(want, &sarif.Replacement{
		DeletedRegion: &sarif.Region{
			StartLine: 4,
			EndLine:   4,
		},
		InsertedContent: &sarif.ArtifactContent{
			Text: "1\n",
		},
	})

	slices.Reverse(want)
	got := replacementsForDiff(a, b)

	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("Wrong parsed diff (-want +got):\n%s", d)
	}
}
