// Copyright 2026 The Shac Authors
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
	"maps"
	"slices"

	"go.starlark.net/starlark"
)

const PROPERTY_FIELD_NAME = "allowed_findings_properties"

type findingsPropertyBag struct {
	unpackedProperties map[string]string

	allowedProperties map[string]bool
}

// Unpack implements starlark.Unpacker
func (fpb *findingsPropertyBag) Unpack(v starlark.Value) error {
	dict, ok := v.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("got %s, want dict", v.Type())
	}
	if dict == nil {
		return nil
	}
	if len(fpb.allowedProperties) == 0 && dict.Len() > 0 {
		return fmt.Errorf("no properties are supported in %s field", PROPERTY_FIELD_NAME)
	}
	m, err := toMap(dict, fpb.allowedProperties)
	if err != nil {
		return err
	}
	fpb.unpackedProperties = m
	return nil
}

func toMap(d *starlark.Dict, allowedProperties map[string]bool) (map[string]string, error) {

	res := make(map[string]string)
	for _, item := range d.Items() {
		k, val := item[0], item[1]
		key, ok := k.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("dict key must be string, got %s", k.Type())
		}
		strKey := key.GoString()
		_, exists := allowedProperties[string(strKey)]
		if !exists {
			allowedNames := slices.Collect(maps.Keys(allowedProperties))
			slices.Sort(allowedNames)
			return nil, fmt.Errorf("key %q not found in %s", strKey, PROPERTY_FIELD_NAME)
		}
		goVal, err := toGoValue(val)
		if err != nil {
			return nil, fmt.Errorf("property %q: %s", strKey, err)
		}
		res[strKey] = goVal
	}
	return res, nil
}

func toGoValue(v starlark.Value) (string, error) {
	switch val := v.(type) {
	case starlark.String:
		return val.GoString(), nil
	default:
		return "", fmt.Errorf("unsupported starlark type: %s, expected str", v.Type())
	}
}
