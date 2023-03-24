// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.chromium.org/luci/common/errors"
)

func TestLoad_Backtrace(t *testing.T) {
	err := Load(context.Background(), "testdata", "backtrace.star")
	if err == nil {
		t.Fatal("expected a failure")
	}
	if s := err.Error(); s != "inner" {
		t.Fatal(s)
	}
	var errs errors.MultiError
	if !errors.As(err, &errs) {
		t.Fatal("not a MultiError")
	}
	if len(errs) != 1 {
		t.Fatal("expected one wrapped error")
	}
	var err2 BacktracableError
	if !errors.As(errs[0], &err2) {
		t.Fatal("not a backtracable error")
	}
	want := `Traceback (most recent call last):
  //backtrace.star:11:4: in <toplevel>
  //backtrace.star:9:6: in fn1
  //backtrace.star:6:7: in fn2
  <builtin>: in fail
Error: inner`
	if diff := cmp.Diff(want, err2.Backtrace()); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}

func TestLoad_Empty(t *testing.T) {
	if err := Load(context.Background(), "testdata", "empty.star"); err == nil {
		t.Fatal("expected a failure")
	} else if s := err.Error(); s != "did you forget to call register_check?" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "io_read_file.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//io_read_file.star:7] {\"key\": \"value\"}\n" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File_Abs(t *testing.T) {
	if err := Load(context.Background(), "testdata", "io_read_file_abs.star"); err == nil {
		t.Fatal("expected a failure")
		// TODO(maruel): Fix the error to include the call site.
	} else if s := err.Error(); s != "do not use absolute path" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File_Escape(t *testing.T) {
	if err := Load(context.Background(), "testdata", "io_read_file_escape.star"); err == nil {
		t.Fatal("expected a failure")
		// TODO(maruel): Fix the error to include the call site.
	} else if s := err.Error(); s != "cannot escape root" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File_Inexistant(t *testing.T) {
	p, err := filepath.Abs(filepath.Join("testdata", "inexistant"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Load(context.Background(), "testdata", "io_read_file_inexistant.star"); err == nil {
		t.Fatal("expected a failure")
		// TODO(maruel): Fix the error to include the call site.
	} else if s := err.Error(); s != "open "+p+": no such file or directory" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File_Missing_Arg(t *testing.T) {
	if err := Load(context.Background(), "testdata", "io_read_file_missing_arg.star"); err == nil {
		t.Fatal("expected a failure")
		// TODO(maruel): Fix the error to include the call site.
	} else if s := err.Error(); s != "read_file: got 0 arguments, want 1" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File_Unclean(t *testing.T) {
	if err := Load(context.Background(), "testdata", "io_read_file_unclean.star"); err == nil {
		t.Fatal("expected a failure")
		// TODO(maruel): Fix the error to include the call site.
	} else if s := err.Error(); s != "pass cleaned path" {
		t.Fatal(s)
	}
}

func TestLoad_IO_Read_File_Windows(t *testing.T) {
	if err := Load(context.Background(), "testdata", "io_read_file_windows.star"); err == nil {
		t.Fatal("expected a failure")
		// TODO(maruel): Fix the error to include the call site.
	} else if s := err.Error(); s != "use POSIX style path" {
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

func TestLoad_Register_Check_Kwargs(t *testing.T) {
	if err := Load(context.Background(), "testdata", "register_check_kwargs.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "register_check: unexpected keyword arguments" {
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
