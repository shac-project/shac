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
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
)

func TestDocument_Validate(t *testing.T) {
	t.Parallel()
	data := []struct {
		in  string
		err string
	}{
		// Dependency.Validate().
		{
			"requirements {\n" +
				"  direct {\n" +
				"  }\n" +
				"}\n",
			"direct require block #1: url must be set",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example\"\n" +
				"  }\n" +
				"}\n",
			"direct require block #1: url is invalid: a path is required",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"  }\n" +
				"}\n",
			"direct require block #1: version must be set",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    alias: \"example.com/bar\"\n" +
				"  }\n" +
				"}\n",
			"direct require block #1: alias is invalid",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"^\"\n" +
				"  }\n" +
				"}\n",
			"direct require block #1: version is invalid",
		},

		// Known.Validate()
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: url must be set",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"^\"\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: url is invalid: unclean url ^",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/foo\"\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: there must be at least on seen entry",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #1: version must be set",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/foo\"\n" +
				"    seen {\n" +
				"      version: \"<\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #1: version is invalid",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #1: digest must be set",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"123\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #1: digest is invalid, must start with \"h1:\"",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:a\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #1: digest is invalid, illegal base64 data at input byte 0",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:AAAAAAAAAAAAAAAAAAAAAA==\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #1: digest is invalid, expected 32 bytes, got 16",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #1: seen block #2: version must be sorted",
		},

		// Document.Validate()
		{
			"",
			"",
		},
		{
			"vendor_path: \"\"\n",
			"",
		},
		{
			"vendor_path: \"foo\"\n",
			"",
		},
		{
			"min_shac_version: \"1000\"\n",
			func() string {
				return fmt.Sprintf(
					"min_shac_version specifies unsupported version \"1000\", running %d.%d.%d",
					Version[0],
					Version[1],
					Version[2],
				)
			}(),
		},
		{
			"min_shac_version: \"1.2.c\"\n",
			"min_shac_version is invalid",
		},
		{
			"min_shac_version: \"1.2.3.4\"\n",
			"min_shac_version is invalid",
		},
		{
			"vendor_path: \"foo/../bar\"\n",
			"vendor_path foo/../bar is not clean",
		},
		{
			"requirements {\n" +
				"  indirect {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n",
			"cannot have indirect dependency without direct one",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n",
			"direct require block #2: example.com/bar was already listed",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"  indirect {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n",
			"indirect require block #1: example.com/bar was already listed",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n",
			"dependency example.com/bar doesn't have a known block",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"  indirect {\n" +
				"    url: \"example.com/foo\"\n" +
				"    alias: \"example\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"dependency example.com/foo doesn't have a known block",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"  indirect {\n" +
				"  }\n" +
				"}\n",
			"indirect require block #1: url must be set",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    alias: \"hi\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"  direct {\n" +
				"    url: \"example.com/foo\"\n" +
				"    alias: \"hi\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n",
			"direct require block #2: alias hi was already listed",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    alias: \"hi\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"  indirect {\n" +
				"    url: \"example.com/foo\"\n" +
				"    alias: \"hi\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n",
			"indirect require block #1: alias hi was already listed",
		},
		{
			"sum {\n" +
				"  known {\n" +
				"  }\n" +
				"}\n",
			"cannot have sum without at least one dependency",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"abc\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"dependency example.com/bar doesn't have a known version 123",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"  }\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"sum known block #2: example.com/bar was already listed",
		},
		{
			"requirements {\n" +
				"  direct {\n" +
				"    url: \"example.com/bar\"\n" +
				"    version: \"123\"\n" +
				"  }\n" +
				"}\n" +
				"sum {\n" +
				"  known {\n" +
				"    url: \"example.com/bar\"\n" +
				"    seen {\n" +
				"      version: \"123\"\n" +
				"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			"",
		},
	}
	for i, l := range data {
		l := l
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			t.Log(l.in)
			doc := Document{}
			if err := prototext.Unmarshal([]byte(l.in), &doc); err != nil {
				t.Fatal(err)
			}
			if err := doc.Validate(); err != nil {
				if diff := cmp.Diff(l.err, err.Error()); diff != "" {
					t.Fatalf("mismatch (-want +got):\n%s", diff)
				}
			} else if l.err != "" {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSumDigest(t *testing.T) {
	t.Parallel()
	in := "requirements {\n" +
		"  direct {\n" +
		"    url: \"example.com/bar\"\n" +
		"    version: \"123\"\n" +
		"  }\n" +
		"}\n" +
		"sum {\n" +
		"  known {\n" +
		"    url: \"example.com/bar\"\n" +
		"    seen {\n" +
		"      version: \"1\"\n" +
		"      digest: \"h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8=\"\n" +
		"    }\n" +
		"    seen {\n" +
		"      version: \"123\"\n" +
		"      digest: \"h1:5Zoc/QRtKVWzQhOtBMvqHzDpF6irO9z98xDceosuGiQ=\"\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	doc := Document{}
	if err := prototext.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	data := []struct {
		version string
		digest  string
	}{
		{"1", "h1:aTJP/BKFRt3cFy4roLF+fH8j9zClpPRrn/UIuwM/6y8="},
		{"123", "h1:5Zoc/QRtKVWzQhOtBMvqHzDpF6irO9z98xDceosuGiQ="},
		{"999", ""},
	}
	for _, l := range data {
		if d := doc.Sum.Digest("example.com/bar", l.version); d != l.digest {
			t.Fatalf("%s: want %s, got %s", l.version, l.digest, d)
		}
	}
}

func TestCleanURL(t *testing.T) {
	t.Parallel()
	data := []struct {
		in   string
		want string
		err  error
	}{
		{
			"example.com/foo",
			"https://example.com/foo",
			nil,
		},
		{
			".foo:",
			"",
			errors.New("parse \".foo:\": first path segment in URL cannot contain colon"),
		},
		{
			"example.com",
			"",
			errors.New("a path is required"),
		},
		{
			"https://foo",
			"",
			errors.New("unexpected scheme for https://foo"),
		},
		{
			"foo?bar",
			"",
			errors.New("unexpected query for foo?bar"),
		},
		{
			// TODO(maruel): Surprising.
			"example.com/foo?",
			"https://example.com/foo?",
			nil,
		},
		{
			"foo#",
			"",
			errors.New("unclean url foo#"),
		},
		{
			"foo#bar",
			"",
			errors.New("unexpected fragment for foo#bar"),
		},
	}
	for i := range data {
		i := i
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			got, err := cleanURL(data[i].in)
			if !errEqual(data[i].err, err) {
				t.Errorf("mismatch:\nwant: %s\ngot:  %s", data[i].err, err)
			}
			if diff := cmp.Diff(data[i].want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
