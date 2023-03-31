// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetDoc(t *testing.T) {
	// This is a state change detector.
	got := getDoc()
	b, err := os.ReadFile(filepath.Join("..", "..", "doc", "stdlib.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := string(b)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("mismatch (+want -got):\n%s", diff)
	}
}
