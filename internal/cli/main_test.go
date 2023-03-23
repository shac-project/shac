// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cli

import "testing"

// TODO(maruel): Real testing.

func TestMain(t *testing.T) {
	if Main(nil) == nil {
		t.Fatal("expected error")
	}
}
