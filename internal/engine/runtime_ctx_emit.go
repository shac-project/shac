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
	var argspan starlark.Tuple
	var argreplacements starlark.Tuple
	if err := starlark.UnpackArgs(name, args, kwargs,
		"level", &arglevel,
		"message", &argmessage,
		"filepath?", &argfilepath,
		"span?", &argspan,
		"replacements?", &argreplacements,
	); err != nil {
		return err
	}
	level := Level(string(arglevel))
	if !level.isValid() {
		return fmt.Errorf("a valid level is required, use one of %q, %q or %q", Notice, Warning, Error)
	}
	message := string(argmessage)
	if len(message) == 0 {
		return errors.New("a message is required")
	}
	file := string(argfilepath)
	span := starlarkToSpan(argspan)
	if span.Start.Line == -1 || span.End.Line == -1 {
		return errors.New("invalid span, expect ((line, col), (line, col))")
	}
	replacements := tupleToString(argreplacements)
	if replacements == nil {
		return errors.New("invalid replacements, expect tuple of str")
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

func starlarkToSpan(t starlark.Tuple) Span {
	s := Span{Start: Cursor{Line: -1}, End: Cursor{Line: -1}}
	if l := len(t); l >= 1 && l <= 2 {
		s.Start.Line, s.Start.Col = tupleTo2Int(t[0])
		if l == 2 {
			s.End.Line, s.End.Col = tupleTo2Int(t[1])
		} else {
			s.End = s.Start
		}
	}
	return s
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

// tupleTo2Int returns -1 on failure.
func tupleTo2Int(v starlark.Value) (int, int) {
	t, ok := v.(starlark.Tuple)
	if !ok || len(t) != 2 {
		return -1, -1
	}
	i := valueToInt(t[0])
	j := valueToInt(t[1])
	if j == -1 {
		i = -1
	}
	return i, j
}

// valueToInt returns -1 on failure.
func valueToInt(v starlark.Value) int {
	k, ok := v.(starlark.Int)
	if !ok {
		return -1
	}
	j, ok := k.Int64()
	const maxInt = int64(int(^uint(0) >> 1))
	if !ok || j < 0 || j > maxInt {
		return -1
	}
	return int(j)
}
