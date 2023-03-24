// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

func TestLoad_Minimal(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "minimal.star"); err != nil {
		t.Fatal(err)
	}
	v := fmt.Sprintf("(%d, %d, %d)", version[0], version[1], version[2])
	if s := b.String(); s != "[//minimal.star:5] "+v+"\n" {
		t.Fatal(s)
	}
}

func TestLoad_Error(t *testing.T) {
	if err := Load(context.Background(), "testdata", "error.star"); err == nil {
		t.Fatal("expected error")
	} else if e := err.Error(); e != "//error.star:5:1: undefined: error" {
		t.Fatal(e)
	}
}

func getErrPrint(t *testing.T) *bytes.Buffer {
	old := stderrPrint
	t.Cleanup(func() {
		stderrPrint = old
	})
	b := &bytes.Buffer{}
	stderrPrint = b
	return b
}

type panicOnWrite struct{}

func (panicOnWrite) Write([]byte) (int, error) {
	panic("unexpected write")
}

func init() {
	// Catch unexpected stderrPrint usage.
	stderrPrint = panicOnWrite{}
}
