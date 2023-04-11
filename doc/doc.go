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
