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
	"fmt"
	"strconv"
	"strings"
)

type shacVersion [3]int

var (
	// Version is the current tool version.
	//
	// TODO(maruel): Add proper version, preferably from git tag.
	Version = shacVersion{0, 1, 12}
)

func (v shacVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func parseVersion(s string) []int {
	var out []int
	for _, x := range strings.Split(s, ".") {
		i, err := strconv.Atoi(x)
		if err != nil {
			return nil
		}
		out = append(out, i)
	}
	return out
}
