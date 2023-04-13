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
	"regexp"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// ctxReMatch implements ctx.re.match.
//
// Returns a struct with the first match and its capturing groups. Returns None
// otherwise.
//
// It uses the RE2 engine as specified at https://golang.org/s/re2syntax.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxReMatch(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	str, r, err := reCommonPreamble(name, args, kwargs)
	if err != nil {
		return nil, err
	}
	m := r.FindStringSubmatchIndex(str)
	if m == nil {
		return starlark.None, nil
	}
	return matchToGroup(str, m), nil
}

// ctxReAllMatches implements ctx.re.allmatches.
//
// It returns a tuple of structs with all the matches and their capturing
// groups. If the file is large or the search is expected to end early, use
// ctx.re.match instead.
//
// It uses the RE2 engine as specified at https://golang.org/s/re2syntax.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func ctxReAllMatches(ctx context.Context, s *shacState, name string, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	str, r, err := reCommonPreamble(name, args, kwargs)
	if err != nil {
		return nil, err
	}
	matches := r.FindAllStringSubmatchIndex(str, -1)
	// Always return a tuple even if no match is found to make client code
	// simpler.
	out := make(starlark.Tuple, len(matches))
	for i, match := range matches {
		// Create a struct for each match.
		out[i] = matchToGroup(str, match)
	}
	return out, nil
}

// matchToGroup creates a struct for the match.
//
// It expects the return value from regexp.Regexp.FindStringSubmatchIndex.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func matchToGroup(s string, groups []int) starlark.Value {
	g := make(starlark.Tuple, len(groups)/2)
	for j := 0; j < len(groups)/2; j++ {
		start, end := groups[2*j], groups[2*j+1]
		// Group indices will be -1 if a capture group was optional and did not
		// match, e.g. `ctx.re.match(r"a(b)?", "a")`.
		if start < 0 && end < 0 {
			g[j] = starlark.None
		} else {
			g[j] = starlark.String(s[start:end])
		}
	}
	return starlarkstruct.FromStringDict(starlark.String("match"),
		starlark.StringDict{
			// offset from the initial string in bytes.
			"offset": starlark.MakeInt(groups[0]),
			"groups": g,
		})
}

// reCommonPreamble implements the common code for functions in ctx.re.*.
//
// Make sure to update //doc/stdlib.star whenever this function is modified.
func reCommonPreamble(name string, args starlark.Tuple, kwargs []starlark.Tuple) (string, *regexp.Regexp, error) {
	var argpattern, argstr starlark.String
	if err := starlark.UnpackArgs(name, args, kwargs, "pattern", &argpattern, "str", &argstr); err != nil {
		return "", nil, err
	}
	r, err := reCache.compile(string(argpattern))
	if err != nil {
		return "", nil, err
	}
	return string(argstr), r, nil
}

// Support functions.

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
