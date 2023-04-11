// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"fmt"

	"go.chromium.org/luci/starlark/interpreter"
	"go.starlark.net/starlark"
)

func ctxEmitAnnotation(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var arglevel starlark.String
	var argmessage starlark.String
	var argfilepath starlark.String
	var argspan starlark.Tuple
	var argreplacements starlark.Tuple
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"level", &arglevel,
		"message", &argmessage,
		"filepath?", &argfilepath,
		"span?", &argspan,
		"replacements?", &argreplacements,
	); err != nil {
		return nil, err
	}
	level := Level(string(arglevel))
	if !level.isValid() {
		return nil, fmt.Errorf("%s: a valid level is required, use one of %q, %q or %q", fn.Name(), Notice, Warning, Error)
	}
	message := string(argmessage)
	if len(message) == 0 {
		return nil, fmt.Errorf("%s: a message is required", fn.Name())
	}
	file := string(argfilepath)
	span := starlarkToSpan(argspan)
	if span.Start.Line == -1 || span.End.Line == -1 {
		return nil, fmt.Errorf("%s: invalid span, expect ((line, col), (line, col))", fn.Name())
	}
	replacements := tupleToString(argreplacements)
	if replacements == nil {
		return nil, fmt.Errorf("%s: invalid replacements, expect tuple of str", fn.Name())
	}
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	c := ctxCheck(ctx)
	if level == "error" {
		c.hadError = true
	}
	if err := s.r.EmitAnnotation(ctx, c.name, level, message, file, span, replacements); err != nil {
		return nil, fmt.Errorf("%s: failed to emit: %w", fn.Name(), err)
	}
	return starlark.None, nil
}

func ctxEmitArtifact(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var argfilepath starlark.String
	var argcontent starlark.Value = starlark.None
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"filepath", &argfilepath,
		"content?", &argcontent,
	); err != nil {
		return nil, err
	}
	f := string(argfilepath)
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
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
			return nil, fmt.Errorf("%s: %w", fn.Name(), err)
		}
		if content, err = readFile(dst, -1); err != nil {
			return nil, fmt.Errorf("%s: %w", fn.Name(), err)
		}
	default:
		return nil, fmt.Errorf("%s: for parameter \"content\": got %s, want str or bytes", fn.Name(), argcontent.Type())
	}
	c := ctxCheck(ctx)
	if err := s.r.EmitArtifact(ctx, c.name, f, content); err != nil {
		return nil, fmt.Errorf("%s: failed to emit: %w", fn.Name(), err)
	}
	return starlark.None, nil
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
