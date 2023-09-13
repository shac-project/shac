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

package cli

import (
	"encoding/json"
	"errors"
	"strings"

	flag "github.com/spf13/pflag"
)

type stringMapFlag map[string]string

var _ flag.Value = (*stringMapFlag)(nil)

func (v stringMapFlag) String() string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (v stringMapFlag) Set(s string) error {
	name, value, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return errors.New("must be of the form key=value")
	}
	if _, ok := v[name]; ok {
		return errors.New("duplicate key")
	}
	v[name] = value
	return nil
}

func (v stringMapFlag) Type() string {
	return "vars"
}
