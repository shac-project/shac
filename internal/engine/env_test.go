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
	"errors"
	"strconv"
	"testing"
)

func TestParseSourceKey(t *testing.T) {
	t.Parallel()
	data := []struct {
		parent sourceKey
		in     string
		want   sourceKey
		err    error
	}{
		{
			err: errors.New("empty reference"),
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//shac.star"},
			in:     "//foo.star",
			want:   sourceKey{orig: "//foo.star", pkg: "__main__", relpath: "foo.star"},
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/shac.star"},
			in:     "../baz.star",
			want:   sourceKey{orig: "../baz.star", pkg: "__main__", relpath: "/baz.star"},
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "../baz.star",
			want:   sourceKey{orig: "../baz.star", pkg: "__main__", relpath: "/baz.star"},
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn",
			want:   sourceKey{orig: "@fqdn", pkg: "fqdn", relpath: "pkg.star"},
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn/",
			err:    errors.New("illegal external reference trailing \"/\": @fqdn/"),
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn//",
			err:    errors.New("illegal external reference path empty: @fqdn//"),
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn//foo.star",
			want:   sourceKey{orig: "@fqdn//foo.star", pkg: "fqdn", relpath: "foo.star"},
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn//foo/../bar.star",
			err:    errors.New("illegal external reference path containing \"..\": @fqdn//foo/../bar.star"),
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn//foo//bar.star",
			err:    errors.New("illegal external reference path containing \"//\": @fqdn//foo//bar.star"),
		},
		{
			parent: sourceKey{pkg: "__main__", relpath: "//foo/bar.star"},
			in:     "@fqdn//foo/internal/bar.star",
			err:    errors.New("illegal external reference path containing \"internal\": @fqdn//foo/internal/bar.star"),
		},
	}
	for i, l := range data {
		l := l
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			got, err := parseSourceKey(l.parent, l.in)
			if !errEqual(l.err, err) {
				t.Errorf("mismatch\nwant: %s\ngot:  %s", l.err, err)
			}
			if l.err == nil && l.want != got {
				t.Errorf("mismatch\nwant: %# v\ngot:  %# v", l.want, got)
			}
		})
	}
}

func errEqual(a, b error) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Error() == b.Error()
}
