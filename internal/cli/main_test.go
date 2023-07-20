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
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestMainHelp(t *testing.T) {
	data := []struct {
		args []string
		want string
	}{
		{nil, "Usage of shac:\n"},
		{[]string{"shac"}, "Usage of shac:\n"},
		{[]string{"shac", "--help"}, "Usage of shac:\n"},
		{[]string{"shac", "check", "--help"}, "Usage of shac check:\n"},
		{[]string{"shac", "fix", "--help"}, "Usage of shac fix:\n"},
		{[]string{"shac", "fmt", "--help"}, "Usage of shac fmt:\n"},
		{[]string{"shac", "doc", "--help"}, "Usage of shac doc:\n"},
	}
	for i, line := range data {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			b := getBuf(t)
			if Main(line.args) == nil {
				t.Fatal("expected error")
			}
			if s := b.String(); !strings.HasPrefix(s, line.want) {
				t.Fatalf("Got:\n%q", s)
			}
		})
	}
}

type panicWrite struct{}

func (panicWrite) Write(b []byte) (int, error) {
	panic("unexpected write!")
}

func getBuf(t *testing.T) *bytes.Buffer {
	old := helpOut
	t.Cleanup(func() {
		helpOut = old
	})
	b := &bytes.Buffer{}
	helpOut = b
	return b
}

func init() {
	helpOut = panicWrite{}
}
