// Copyright 2023 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"regexp"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// reMatch implements shac.re.match.
//
// Returns a struct with the first match and its capturing groups. Returns None
// otherwise.
//
// It uses the RE2 engine as specified at https://golang.org/s/re2syntax.
func reMatch(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	s, r, err := reCommonPreamble(fn, args, kwargs)
	if err != nil {
		return starlark.None, err
	}
	m := r.FindStringSubmatchIndex(s)
	if m == nil {
		return starlark.None, nil
	}
	return matchToGroup(s, m), nil
}

// reAllMatches implements shac.re.allmatches.
//
// It returns a tuple of structs with all the matches and their capturing
// groups. If the file is large or the search is expected to end early, use
// shac.re.match instead.
//
// It uses the RE2 engine as specified at https://golang.org/s/re2syntax.
func reAllMatches(th *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	s, r, err := reCommonPreamble(fn, args, kwargs)
	if err != nil {
		return starlark.None, err
	}
	matches := r.FindAllStringSubmatchIndex(s, -1)
	// Always return a tuple even if no match is found to make client code
	// simpler.
	out := make(starlark.Tuple, len(matches))
	for i, match := range matches {
		// Create a struct for each match.
		out[i] = matchToGroup(s, match)
	}
	return out, nil
}

// matchToGroup creates a struct for the match.
//
// It expects the return value from regexp.Regexp.FindStringSubmatchIndex.
func matchToGroup(s string, groups []int) starlark.Value {
	g := make(starlark.Tuple, len(groups)/2)
	for j := 0; j < len(groups)/2; j++ {
		g[j] = starlark.String(s[groups[2*j]:groups[2*j+1]])
	}
	return starlarkstruct.FromStringDict(starlark.String("match"),
		starlark.StringDict{
			// offset from the initial string in bytes.
			"offset": starlark.MakeInt(groups[0]),
			"groups": g,
		})
}

func reCommonPreamble(fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (string, *regexp.Regexp, error) {
	var argpattern, argstr starlark.String
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "pattern", &argpattern, "str", &argstr); err != nil {
		return "", nil, err
	}
	r, err := reCache.compile(string(argpattern))
	return string(argstr), r, err
}

var reCache = reCacheImpl{r: map[string]*regexp.Regexp{}}

type reCacheImpl struct {
	m sync.Mutex
	r map[string]*regexp.Regexp
}

func (c *reCacheImpl) compile(pat string) (*regexp.Regexp, error) {
	c.m.Lock()
	defer c.m.Unlock()
	if r := c.r[pat]; r != nil {
		return r, nil
	}
	r, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}
	c.r[pat] = r
	return r, nil
}
