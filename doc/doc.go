// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package doc doesn't implement anything by itself.
//
// It serves as a repository for documenting the standard library.
package doc

import _ "embed"

// StdlibSrc contains the shac runtime standard library pseudo-code.
//
// This is not the real code, but a starlark representation of the Go native
// implementation for documentation purpose.
//
//go:embed stdlib.star
var StdlibSrc string
