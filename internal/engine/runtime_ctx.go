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
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"

	"go.starlark.net/starlark"
)

// getCtx returns the ctx object to pass to a registered check callback.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func getCtx() starlark.Value {
	return toValue("ctx", starlark.StringDict{
		// Implemented in runtime_ctx_emit.go
		"emit": toValue("ctx.emit", starlark.StringDict{
			"annotation": newBuiltinNone("ctx.emit.annotation", ctxEmitAnnotation),
			"artifact":   newBuiltinNone("ctx.emit.artifact", ctxEmitArtifact),
		}),
		"io": toValue("ctx.io", starlark.StringDict{
			"read_file": newBuiltin("ctx.io.read_file", ctxIoReadFile),
		}),
		"os": toValue("ctx.os", starlark.StringDict{
			"exec": newBuiltin("ctx.os.exec", ctxOsExec),
		}),
		// Implemented in runtime_ctx_re.go
		"re": toValue("ctx.re", starlark.StringDict{
			"match":      newBuiltin("ctx.re.match", ctxReMatch),
			"allmatches": newBuiltin("ctx.re.allmatches", ctxReAllMatches),
		}),
		// Implemented in runtime_ctx_scm.go
		"scm": toValue("ctx.scm", starlark.StringDict{
			"affected_files": newBuiltin("ctx.scm.affected_files", ctxScmAffectedFiles),
			"all_files":      newBuiltin("ctx.scm.all_files", ctxScmAllFiles),
		}),
	})
}

// ctxIoReadFile implements native function ctx.io.read_file().
//
// Use POSIX style relative path. "..", "\" and absolute paths are denied.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxIoReadFile(ctx context.Context, s *state, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argfilepath starlark.String
	var argsize starlark.Int
	if err := starlark.UnpackArgs(name, args, kwargs,
		"filepath", &argfilepath,
		"size?", &argsize,
	); err != nil {
		return nil, err
	}
	size, ok := argsize.Int64()
	if !ok {
		return nil, fmt.Errorf("for parameter \"size\": %s is an invalid size", argsize)
	}
	dst, err := absPath(string(argfilepath), s.inputs.root)
	if err != nil {
		return nil, fmt.Errorf("for parameter \"filepath\": %s %w", argfilepath, err)
	}
	b, err := readFile(dst, size)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Hide the underlying error for determinism.
			return nil, fmt.Errorf("for parameter \"filepath\": %s not found", argfilepath)
		}
		// Something other than a file not found error, return it as is.
		return nil, fmt.Errorf("for parameter \"filepath\": %s %w", argfilepath, err)
	}
	// TODO(maruel): Use unsafe conversion to save a memory copy.
	return starlark.Bytes(b), nil
}

// ctxOsExec implements the native function ctx.os.exec().
//
// TODO(olivernewman): Return a struct with stdout and stderr in addition to the
// exit code.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxOsExec(ctx context.Context, s *state, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var rawCmd *starlark.List
	var cwd starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs,
		"cmd", &rawCmd,
		"cwd?", &cwd,
	); err != nil {
		return nil, err
	}
	if rawCmd.Len() == 0 {
		return nil, errors.New("cmdline must not be an empty list")
	}

	var parsedCmd []string
	var val starlark.Value
	iter := rawCmd.Iterate()
	defer iter.Done()
	for iter.Next(&val) {
		str, ok := val.(starlark.String)
		if !ok {
			return nil, errors.New("command args must be strings")
		}
		parsedCmd = append(parsedCmd, str.GoString())
	}

	// TODO(olivernewman): Wrap with nsjail on linux.
	//#nosec G204
	cmd := exec.CommandContext(ctx, parsedCmd[0], parsedCmd[1:]...)

	if cwd.GoString() != "" {
		var err error
		cmd.Dir, err = absPath(cwd.GoString(), s.inputs.root)
		if err != nil {
			return nil, err
		}
	} else {
		cmd.Dir = s.inputs.root
	}

	if err := cmd.Run(); err != nil {
		if errExit := (&exec.ExitError{}); errors.As(err, &errExit) {
			return starlark.MakeInt(errExit.ExitCode()), nil
		}
		return nil, err
	}
	return starlark.MakeInt(0), nil
}

// Support functions.

// readFile is similar to os.ReadFile() albeit it limits the amount of data
// returned to max bytes when specified.
//
// On 32 bits, max defaults to 128Mib. On 64 bits, max defaults to 4Gib.
func readFile(name string, max int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	//#nosec G307
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("is a directory")
	}
	size := info.Size()
	if max > 0 && size > max {
		size = max
	}
	if uintSize := 32 << (^uint(0) >> 63); uintSize == 32 {
		if hardMax := int64(128 * 1024 * 1024); size > hardMax {
			size = hardMax
		}
	} else if hardMax := int64(4 * 1024 * 1024 * 1024); size > hardMax {
		size = hardMax
	}
	for data := make([]byte, 0, int(size)); ; {
		n, err := f.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil || len(data) == cap(data) {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
	}
}

// absPath makes a source-relative path absolute, validating it along the way.
func absPath(rel, rootDir string) (string, error) {
	if strings.Contains(rel, "\\") {
		return "", errors.New("use POSIX style path")
	}
	// Package path use POSIX style even on Windows, unlike path/filepath.
	if path.IsAbs(rel) {
		return "", errors.New("do not use absolute path")
	}
	// This is overly zealous. Revisit if it is too much.
	if path.Clean(rel) != rel {
		return "", errors.New("pass cleaned path")
	}
	pathParts := append([]string{rootDir}, strings.Split(rel, "/")...)
	res := path.Join(pathParts...)
	if !strings.HasPrefix(res, rootDir) {
		return "", errors.New("cannot escape root")
	}
	return res, nil
}
