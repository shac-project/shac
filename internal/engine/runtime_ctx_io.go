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
	"path"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
)

// ctxIoReadFile implements native function ctx.io.read_file().
//
// Use POSIX style relative path. "..", "\" and absolute paths are denied.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxIoReadFile(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
	dst := string(argfilepath)
	if !filepath.IsAbs(dst) {
		var err error
		dst, err = absPath(dst, filepath.Join(s.root, s.subdir))
		if err != nil {
			return nil, fmt.Errorf("for parameter \"filepath\": %s %w", argfilepath, err)
		}
	}
	b, err := readFileImpl(dst, size)
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

// ctxIoTempdir implements native function ctx.io.tempdir().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxIoTempdir(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(name, args, kwargs); err != nil {
		return nil, err
	}
	t, err := s.newTempDir()
	return starlark.String(t), err
}

// ctxIoTempfile implements native function ctx.io.tempfile().
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxIoTempfile(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argcontent starlark.Value
	var argname starlark.String = "00001"
	if err := starlark.UnpackArgs(name, args, kwargs,
		"content", &argcontent,
		"name?", &argname,
	); err != nil {
		return nil, err
	}
	var content []byte
	switch v := argcontent.(type) {
	case starlark.Bytes:
		content = unsafeByteSlice(string(v))
	case starlark.String:
		content = unsafeByteSlice(string(v))
	default:
		return nil, fmt.Errorf("for parameter \"content\": got %s, want str or bytes", argcontent.Type())
	}

	// TODO(olivernewman): Consider not creating a new dir for every temp file.
	dir, err := s.newTempDir()
	if err != nil {
		return nil, err
	}
	if filepath.IsAbs(string(argname)) {
		return nil, fmt.Errorf("for parameter \"name\": absolute paths are not allowed")
	}

	path := filepath.Join(dir, string(argname))
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	if err = os.WriteFile(path, content, 0o600); err != nil {
		return nil, err
	}
	return starlark.String(path), err
}

// Support functions.

// readFileImpl is similar to os.ReadFile() albeit it limits the amount of data
// returned to max bytes when specified.
//
// On 32 bits, max defaults to 128Mib. On 64 bits, max defaults to 4Gib.
func readFileImpl(name string, max int64) ([]byte, error) {
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
