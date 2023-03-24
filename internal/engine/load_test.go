// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
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

func TestLoad_IO_Read_File(t *testing.T) {
	b := getErrPrint(t)
	if err := Load(context.Background(), "testdata", "io_read_file.star"); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); s != "[//io_read_file.star:7] {\"key\": \"value\"}\n" {
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

func TestLoad_Register_Check_Recursive(t *testing.T) {
	if err := Load(context.Background(), "testdata", "register_check_recursive.star"); err == nil {
		t.Fatal("expected error")
	} else if s := err.Error(); s != "can't register checks after done loading" {
		t.Fatal(s)
	}
}

// TestTestDataFail runs all the files under testdata/fail/.
func TestTestDataFail(t *testing.T) {
	p := filepath.Join("testdata", "fail")
	d, err := os.ReadDir(p)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(d))
	for i := range d {
		if !d[i].IsDir() {
			got[i] = d[i].Name()
		}
	}
	inexistant, err := filepath.Abs(filepath.Join("testdata", "fail", "inexistant"))
	if err != nil {
		t.Fatal(err)
	}
	data := []struct {
		name string
		err  error
	}{
		{
			"empty.star",
			errors.New("did you forget to call register_check?"),
		},
		{
			"fail.star",
			errors.New("an expected failure"),
		},
		{
			"io_read_file_abs.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("do not use absolute path"),
		},
		{
			"io_read_file_escape.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("cannot escape root"),
		},
		{
			"io_read_file_inexistant.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("open " + inexistant + ": no such file or directory"),
		},
		{
			"io_read_file_missing_arg.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("read_file: got 0 arguments, want 1"),
		},
		{
			"io_read_file_unclean.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("pass cleaned path"),
		},
		{
			"io_read_file_windows.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("use POSIX style path"),
		},
		{
			"register_check_kwargs.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("register_check: unexpected keyword arguments"),
		},
		{
			"register_check_no_arg.star",
			// TODO(maruel): Fix the error to include the call site.
			errors.New("register_check: got 0 arguments, want 1"),
		},
		{
			"syntax_error.star",
			errors.New("//syntax_error.star:5:3: got '//', want primary expression"),
		},
		{
			"undefined_symbol.star",
			errors.New("//undefined_symbol.star:5:1: undefined: undefined_symbol"),
		},
	}
	want := make([]string, len(data))
	for i := range data {
		want[i] = data[i].name
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
	for i := range data {
		t.Run(data[i].name, func(t *testing.T) {
			err := Load(context.Background(), p, data[i].name)
			if !equalError(data[i].err, err) {
				if diff := cmp.Diff(data[i].err, err); diff != "" {
					t.Fatalf("mismatch (+want -got):\n%s", diff)
				}
			}
		})
	}
}

func equalError(a, b error) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Error() == b.Error()
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
