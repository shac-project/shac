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

func TestLoad_Empty(t *testing.T) {
	if err := Load(context.Background(), "testdata", "empty.star"); err == nil {
		t.Fatal("expected a failure")
	} else if s := err.Error(); s != "did you forget to call register_check?" {
		t.Fatal(s)
	}
}

func TestLoad_Fail(t *testing.T) {
	if err := Load(context.Background(), "testdata", "fail.star"); err == nil {
		t.Fatal("expected a failure")
	} else if s := err.Error(); s != "expected failure" {
		t.Fatal(s)
	}
}

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

func TestLoad_Register_Check(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "register_check.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//register_check.star:6] running\n" {
		t.Fatal(s)
	}
}

func TestLoad_Register_Check_No_Arg(t *testing.T) {
	if err := Load(context.Background(), "testdata", "register_check_no_arg.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "register_check: got 0 arguments, want 1" {
		t.Fatal(s)
	}
}

func TestLoad_Register_Check_Recursive(t *testing.T) {
	if err := Load(context.Background(), "testdata", "register_check_recursive.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "can't register checks after done loading" {
		t.Fatal(s)
	}
}

func TestLoad_Syntax_Error(t *testing.T) {
	if err := Load(context.Background(), "testdata", "syntax_error.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "//syntax_error.star:5:3: got '//', want primary expression" {
		t.Fatal(s)
	}
}

func TestLoad_Undefined_Symbol(t *testing.T) {
	if err := Load(context.Background(), "testdata", "undefined_symbol.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "//undefined_symbol.star:5:1: undefined: undefined_symbol" {
		t.Fatal(s)
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
