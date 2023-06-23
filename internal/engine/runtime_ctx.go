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

package engine

import (
	"runtime"

	"go.starlark.net/starlark"
)

// getCtx returns the ctx object to pass to a registered check callback.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func getCtx(root string) starlark.Value {
	return toValue("ctx", starlark.StringDict{
		// Implemented in runtime_ctx_emit.go
		"emit": toValue("ctx.emit", starlark.StringDict{
			"finding":  newBuiltinNone("ctx.emit.finding", ctxEmitFinding),
			"artifact": newBuiltinNone("ctx.emit.artifact", ctxEmitArtifact),
		}),
		"io": toValue("ctx.io", starlark.StringDict{
			"read_file": newBuiltin("ctx.io.read_file", ctxIoReadFile),
			"tempdir":   newBuiltin("ctx.io.tempdir", ctxIoTempdir),
		}),
		"os": toValue("ctx.os", starlark.StringDict{
			"exec": newBuiltin("ctx.os.exec", ctxOsExec),
			"name": starlark.String(runtime.GOOS),
		}),
		// Implemented in runtime_ctx_re.go
		"re": toValue("ctx.re", starlark.StringDict{
			"match":      newBuiltin("ctx.re.match", ctxReMatch),
			"allmatches": newBuiltin("ctx.re.allmatches", ctxReAllMatches),
		}),
		// Implemented in runtime_ctx_scm.go
		"scm": toValue("ctx.scm", starlark.StringDict{
			"root":           starlark.String(root),
			"affected_files": newBuiltin("ctx.scm.affected_files", ctxScmAffectedFiles),
			"all_files":      newBuiltin("ctx.scm.all_files", ctxScmAllFiles),
		}),
	})
}
