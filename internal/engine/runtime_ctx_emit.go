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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"go.starlark.net/starlark"
)

func ctxEmitFinding(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var arglevel starlark.String
	var argmessage starlark.String
	var argfilepath starlark.String
	var argline starlark.Int
	var argcol starlark.Int
	var argendCol starlark.Int
	var argendLine starlark.Int
	var argreplacements starlark.Sequence
	if err := starlark.UnpackArgs(name, args, kwargs,
		"level", &arglevel,
		"message", &argmessage,
		"filepath?", &argfilepath,
		"line?", &argline,
		"col?", &argcol,
		"end_line?", &argendLine,
		"end_col?", &argendCol,
		"replacements?", &argreplacements,
	); err != nil {
		return err
	}
	level := Level(string(arglevel))
	if !level.isValid() {
		return fmt.Errorf("for parameter \"level\": got %s, want one of %q, %q or %q", arglevel, Notice, Warning, Error)
	}
	message := string(argmessage)
	if len(message) == 0 {
		return fmt.Errorf("for parameter \"message\": got %s, want string", argmessage)
	}
	file := string(argfilepath)
	span := Span{
		Start: Cursor{
			Line: intToInt(argline),
			Col:  intToInt(argcol),
		},
		End: Cursor{
			Line: intToInt(argendLine),
			Col:  intToInt(argendCol),
		},
	}
	if span.Start.Line <= -1 {
		return fmt.Errorf("for parameter \"line\": got %s, line are 1 based", argline)
	} else if span.Start.Col <= -1 {
		return fmt.Errorf("for parameter \"col\": got %s, line are 1 based", argcol)
	} else if span.End.Line <= -1 {
		return fmt.Errorf("for parameter \"end_line\": got %s, line are 1 based", argendLine)
	} else if span.End.Col <= -1 {
		return fmt.Errorf("for parameter \"end_col\": got %s, line are 1 based", argendCol)
	}
	if span.Start.Col == 0 && span.End.Col > 0 {
		return errors.New("for parameter \"end_col\": \"col\" must be specified")
	}
	if span.Start.Line > 0 {
		if file == "" {
			return errors.New("for parameter \"line\": \"filepath\" must be specified")
		}
		if span.End.Line > 0 {
			if span.End.Line < span.Start.Line {
				return errors.New("for parameter \"end_line\": must be greater than or equal to \"line\"")
			} else if span.End.Line == span.Start.Line && span.End.Col > 0 && span.End.Col <= span.Start.Col {
				return errors.New("for parameter \"end_col\": must be greater than \"col\"")
			}
		} else if span.End.Col > 0 {
			// If end_col is set but end_line is unset, assume that end_line is
			// equal to line.
			span.End.Line = span.Start.Line
		}
	} else {
		if span.End.Line > 0 {
			return errors.New("for parameter \"end_line\": \"line\" must be specified")
		}
		if span.Start.Col > 0 {
			return errors.New("for parameter \"col\": \"line\" must be specified")
		}
	}
	var replacements []string
	if argreplacements != nil {
		if file == "" {
			return errors.New("for parameter \"replacements\": \"filepath\" must be specified")
		}
		if replacements = sequenceToStrings(argreplacements); replacements == nil {
			return fmt.Errorf("for parameter \"replacements\": got %s, want sequence of str", argreplacements.Type())
		}
		if len(replacements) > 100 {
			return fmt.Errorf("for parameter \"replacements\": excessive number (%d) of replacements", len(replacements))
		}
	}
	c := ctxCheck(ctx)
	if c.highestLevel == "" || level == Error || (level == Warning && c.highestLevel != Error) {
		c.highestLevel = level
	}
	root := ""
	if file != "" {
		root = filepath.Join(s.root, s.subdir)
		// The file must be tracked by scm.
		f, err := s.scm.allFiles(ctx, false)
		if err != nil {
			return err
		}
		if _, found := sort.Find(len(f), func(i int) int { return strings.Compare(file, f[i].relpath()) }); !found {
			return fmt.Errorf("for parameter \"filepath\": %s is not tracked", argfilepath)
		}
	}
	if err := s.r.EmitFinding(ctx, c.name, level, message, root, file, span, replacements); err != nil {
		return fmt.Errorf("failed to emit: %w", err)
	}
	return nil
}

func ctxEmitArtifact(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var argfilepath starlark.String
	var argcontent starlark.Value = starlark.None
	if err := starlark.UnpackArgs(name, args, kwargs,
		"filepath", &argfilepath,
		"content?", &argcontent,
	); err != nil {
		return err
	}
	f := string(argfilepath)
	var content []byte
	root := ""
	switch v := argcontent.(type) {
	case starlark.Bytes:
		content = unsafeByteSlice(string(v))
	case starlark.String:
		content = unsafeByteSlice(string(v))
	case starlark.NoneType:
		root = filepath.Join(s.root, s.subdir)
		dst, err := absPath(f, root)
		if err != nil {
			return fmt.Errorf("for parameter \"filepath\": %s %w", argfilepath, err)
		}
		// Make sure the file exist, but do not load it.
		if info, err := os.Stat(dst); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// Hide the underlying error for determinism.
				return fmt.Errorf("for parameter \"filepath\": %q not found", f)
			}
			// Something other than a file not found error, return it as is.
			return fmt.Errorf("for parameter \"filepath\": %w", err)
		} else if info.IsDir() {
			return fmt.Errorf("for parameter \"filepath\": %q is a directory", f)
		}
	default:
		return fmt.Errorf("for parameter \"content\": got %s, want str or bytes", argcontent.Type())
	}
	c := ctxCheck(ctx)
	if err := s.r.EmitArtifact(ctx, c.name, root, f, content); err != nil {
		return fmt.Errorf("failed to emit: %w", err)
	}
	return nil
}

// sequenceToStrings converts a starlark sequence (list, tuple) into a list of strings.
func sequenceToStrings(s starlark.Sequence) []string {
	out := make([]string, 0, s.Len())
	iter := s.Iterate()
	var x starlark.Value
	for iter.Next(&x) {
		s, ok := x.(starlark.String)
		if !ok {
			return nil
		}
		out = append(out, string(s))
	}
	return out
}

// intToInt returns -1 on failure.
func intToInt(i starlark.Int) int {
	i64, ok := i.Int64()
	const maxInt = int64(int(^uint(0) >> 1))
	if !ok || i64 < 0 || i64 > maxInt {
		return -1
	}
	return int(i64)
}

func unsafeByteSlice(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
