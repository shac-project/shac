// Copyright 2023 The Shac Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"errors"

	"go.chromium.org/luci/starlark/interpreter"
	"go.starlark.net/starlark"
)

func ctxEmitAnnotation(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var arglevel starlark.String
	var argmessage starlark.String
	var argfile starlark.String
	var argspan starlark.Tuple
	var argreplacements starlark.Tuple
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"level", &arglevel,
		"message", &argmessage,
		"file?", &argfile,
		"span?", &argspan,
		"replacements?", &argreplacements,
	); err != nil {
		return nil, err
	}
	level := string(arglevel)
	switch level {
	case "notice", "warning", "error":
	default:
		return nil, errors.New("a valid level is required, use one of \"notice\", \"warning\" or \"error\"")
	}
	message := string(argmessage)
	if len(message) == 0 {
		return nil, errors.New("a message is required")
	}
	file := string(argfile)
	span := starlarkToSpan(argspan)
	if span.Start.Line == -1 || span.End.Line == -1 {
		return nil, errors.New("invalid span, expect ((line, col), (line, col))")
	}
	replacements := tupleToString(argreplacements)
	if replacements == nil {
		return nil, errors.New("invalid replacements, expect tuple of str")
	}
	ctx := interpreter.Context(th)
	s := ctxState(ctx)
	c := ctxCheck(ctx)
	if err := s.r.EmitAnnotation(ctx, c.name, level, message, file, span, replacements); err != nil {
		// Surface the error to starlark, is it the right thing to do?
		return nil, err
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
