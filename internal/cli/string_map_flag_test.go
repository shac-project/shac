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

package cli

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	flag "github.com/spf13/pflag"
)

func TestStringMapFlag(t *testing.T) {
	t.Parallel()
	data := []struct {
		name    string
		args    []string
		want    stringMapFlag
		wantErr string
	}{
		{
			name: "empty",
			args: nil,
			want: nil,
		},
		{
			name: "one",
			args: []string{"--kv", "x=y"},
			want: stringMapFlag{"x": "y"},
		},
		{
			name: "two",
			args: []string{"--kv", "x=y", "--kv", "a=b"},
			want: stringMapFlag{"x": "y", "a": "b"},
		},
		{
			name: "empty string value",
			args: []string{"--kv", "x="},
			want: stringMapFlag{"x": ""},
		},
		{
			name:    "empty string key",
			args:    []string{"--kv", "=y"},
			wantErr: `invalid argument "=y" for "--kv" flag: must be of the form key=value`,
		},
		{
			name:    "duplicate",
			args:    []string{"--kv", "x=y", "--kv", "x=z"},
			wantErr: `invalid argument "x=z" for "--kv" flag: duplicate key`,
		},
		{
			name:    "malformed",
			args:    []string{"--kv", "xy"},
			wantErr: `invalid argument "xy" for "--kv" flag: must be of the form key=value`,
		},
	}
	for i := range data {
		i := i
		t.Run(data[i].name, func(t *testing.T) {
			m := stringMapFlag{}
			f := flag.NewFlagSet("test", flag.ContinueOnError)
			f.Var(&m, "kv", "")

			err := f.Parse(data[i].args)
			if err != nil {
				if data[i].wantErr == "" {
					t.Fatal(err)
				}
				if diff := cmp.Diff(data[i].wantErr, err.Error()); diff != "" {
					t.Errorf("Unexpected error: %s", diff)
				}
			} else {
				if data[i].wantErr != "" {
					t.Fatalf("Wanted error %q, got nil", data[i].wantErr)
				}
				if diff := cmp.Diff(data[i].want, m, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("unexpected diff:\n%s", diff)
				}
			}
		})
	}
}
