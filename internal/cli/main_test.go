// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
