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

	"go.starlark.net/starlark"
)

func ctxEmitAnnotation(ctx context.Context, s *state, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
	var arglevel starlark.String
	var argmessage starlark.String
	var argfilepath starlark.String
	var argline starlark.Int
	var argcol starlark.Int
	var argendCol starlark.Int
	var argendLine starlark.Int
	var argreplacements starlark.Tuple
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
		if span.End.Line > 0 {
			if span.End.Line < span.Start.Line {
				return errors.New("for parameter \"end_line\": must be greater than \"line\"")
			} else if span.End.Line == span.Start.Line && span.End.Col > 0 && span.End.Col < span.Start.Col {
				return errors.New("for parameter \"end_col\": must be greater than \"col\"")
			}
		}
	} else {
		if span.End.Line > 0 {
			return errors.New("for parameter \"end_line\": \"line\" must be specified")
		}
		if span.Start.Col > 0 {
			return errors.New("for parameter \"col\": \"line\" must be specified")
		}
	}
	replacements := tupleToString(argreplacements)
	if replacements == nil {
		return fmt.Errorf("for parameter \"replacements\": got %s, want tuple of str", argreplacements.Type())
	}
	c := ctxCheck(ctx)
	if level == "error" {
		c.hadError = true
	}
	if err := s.r.EmitAnnotation(ctx, c.name, level, message, file, span, replacements); err != nil {
		return fmt.Errorf("failed to emit: %w", err)
	}
	return nil
}

func ctxEmitArtifact(ctx context.Context, s *state, name string, args starlark.Tuple, kwargs []starlark.Tuple) error {
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
	switch v := argcontent.(type) {
	case starlark.Bytes:
		// TODO(maruel): Use unsafe conversion to save a memory copy.
		content = []byte(v)
	case starlark.String:
		// TODO(maruel): Use unsafe conversion to save a memory copy.
		content = []byte(v)
	case starlark.NoneType:
		dst, err := absPath(f, s.inputs.root)
		if err != nil {
			return err
		}
		if content, err = readFile(dst, -1); err != nil {
			return err
		}
	default:
		return fmt.Errorf("for parameter \"content\": got %s, want str or bytes", argcontent.Type())
	}
	c := ctxCheck(ctx)
	if err := s.r.EmitArtifact(ctx, c.name, f, content); err != nil {
		return fmt.Errorf("failed to emit: %w", err)
	}
	return nil
}

// tupleToString returns nil on failure.
func tupleToString(t starlark.Tuple) []string {
	out := make([]string, len(t))
	for i := range t {
		s, ok := t[i].(starlark.String)
		if !ok {
			return nil
		}
		out[i] = string(s)
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
