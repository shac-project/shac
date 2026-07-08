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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
)

// mockEnv simulates the environment where the Starlark function is registered.
type mockEnv struct {
	gotConfig map[string]string
}

// setConfig is the Go implementation of the Starlark function.
// It expects a "config" keyword argument which it unpacks into a findingsPropertyBag.
func (env *mockEnv) setConfig(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	config := findingsPropertyBag{
		allowedProperties: map[string]bool{"key_str": true},
	}
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "config", &config); err != nil {
		return nil, err
	}
	env.gotConfig = config.unpackedProperties
	return starlark.None, nil
}

func TestUnpackConfig(t *testing.T) {
	env := &mockEnv{}
	predeclared := starlark.StringDict{
		"set_config": starlark.NewBuiltin("set_config", env.setConfig),
	}
	const src = `
set_config(
    config = {
        "key_str": "value",
    }
)
`
	thread := &starlark.Thread{Name: "test-thread"}
	_, err := starlark.ExecFile(thread, "test.star", src, predeclared)
	if err != nil {
		t.Fatalf("ExecFile failed: %v", err)
	}
	if env.gotConfig == nil {
		t.Fatal("Expected config to be unpacked, but it was nil")
	}
	wantConfig := map[string]string{
		"key_str": "value",
	}
	if diff := cmp.Diff(wantConfig, env.gotConfig); diff != "" {
		t.Errorf("Config mismatch (-want +got):\n%s", diff)
	}
}

// TestUnpackConfig_Validation_Success verifies that unpacking succeeds when
// all keys in both dictionaries are valid, and that the resulting structs
// are populated correctly.
func TestUnpackConfig_Validation_Success(t *testing.T) {
	var gotConfigA map[string]string
	var gotConfigB map[string]string
	var gotErr error
	// A mock function that accepts two dicts with different allowedPropertiess
	setConfig := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// Define different allowed keys for each parameter
		configA := findingsPropertyBag{
			allowedProperties: map[string]bool{
				"enabled": true,
				"timeout": true,
			},
		}
		configB := findingsPropertyBag{
			allowedProperties: map[string]bool{
				"url":     true,
				"retries": true,
			},
		}
		gotErr = starlark.UnpackArgs(b.Name(), args, kwargs,
			"config_a", &configA,
			"config_b", &configB,
		)
		if gotErr == nil {
			gotConfigA = configA.unpackedProperties
			gotConfigB = configB.unpackedProperties
		}
		return starlark.None, nil
	}
	predeclared := starlark.StringDict{
		"set_config": starlark.NewBuiltin("set_config", setConfig),
	}
	const src = `
set_config(
    config_a = {"enabled": "yes", "timeout": "30m"},
    config_b = {"url": "localhost", "retries": "none"},
)
`
	thread := &starlark.Thread{Name: "test-thread"}
	_, err := starlark.ExecFile(thread, "test.star", src, predeclared)
	if err != nil {
		t.Fatalf("ExecFile failed: %v", err)
	}
	if gotErr != nil {
		t.Fatalf("Unpack failed: %v", gotErr)
	}
	// Verify Config A
	wantConfigA := map[string]string{
		"enabled": "yes",
		"timeout": "30m",
	}
	if diff := cmp.Diff(wantConfigA, gotConfigA); diff != "" {
		t.Errorf("Config A mismatch (-want +got):\n%s", diff)
	}
	// Verify Config B
	wantConfigB := map[string]string{
		"url":     "localhost",
		"retries": "none",
	}
	if diff := cmp.Diff(wantConfigB, gotConfigB); diff != "" {
		t.Errorf("Config B mismatch (-want +got):\n%s", diff)
	}
}

func TestUnpackConfig_NotADict(t *testing.T) {
	var gotErr error
	setConfig := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var config findingsPropertyBag
		gotErr = starlark.UnpackArgs(b.Name(), args, kwargs, "config", &config)
		return starlark.None, nil
	}

	predeclared := starlark.StringDict{
		"set_config": starlark.NewBuiltin("set_config", setConfig),
	}

	const src = `set_config(config = "not a dict")`
	thread := &starlark.Thread{Name: "test-thread"}
	_, err := starlark.ExecFile(thread, "test.star", src, predeclared)
	if err != nil {
		t.Fatalf("ExecFile failed: %v", err)
	}

	if gotErr == nil {
		t.Fatal("Expected error when passing non-dict, got nil")
	}

	expectedMsg := "got string, want dict"
	if !strings.Contains(gotErr.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, but got: %v", expectedMsg, gotErr)
	}
}

func TestUnpackConfig_NonStringKey(t *testing.T) {
	var gotErr error
	setConfig := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		config := findingsPropertyBag{
			allowedProperties: map[string]bool{"key": true},
		}
		gotErr = starlark.UnpackArgs(b.Name(), args, kwargs, "config", &config)
		return starlark.None, nil
	}

	predeclared := starlark.StringDict{
		"set_config": starlark.NewBuiltin("set_config", setConfig),
	}

	const src = `set_config(config = {123: "value"})`
	thread := &starlark.Thread{Name: "test-thread"}
	_, err := starlark.ExecFile(thread, "test.star", src, predeclared)
	if err != nil {
		t.Fatalf("ExecFile failed: %v", err)
	}

	if gotErr == nil {
		t.Fatal("Expected error when dict has non-string key, got nil")
	}

	expectedMsg := "dict key must be string, got int"
	if !strings.Contains(gotErr.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, but got: %v", expectedMsg, gotErr)
	}
}

func TestUnpackConfig_UnsupportedType(t *testing.T) {
	var gotErr error
	setConfig := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		config := findingsPropertyBag{
			allowedProperties: map[string]bool{"func": true},
		}
		gotErr = starlark.UnpackArgs(b.Name(), args, kwargs, "config", &config)
		return starlark.None, nil
	}

	predeclared := starlark.StringDict{
		"set_config": starlark.NewBuiltin("set_config", setConfig),
	}

	const src = `
def dummy():
    pass
set_config(config = {"func": dummy})
`
	thread := &starlark.Thread{Name: "test-thread"}
	_, err := starlark.ExecFile(thread, "test.star", src, predeclared)
	if err != nil {
		t.Fatalf("ExecFile failed: %v", err)
	}

	if gotErr == nil {
		t.Fatal("Expected error for unsupported type, got nil")
	}

	expectedMsg := `property "func": unsupported starlark type: function, expected str`
	if !strings.Contains(gotErr.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, but got: %v", expectedMsg, gotErr)
	}
}

func TestUnpackConfig_Validation_Failures(t *testing.T) {
	tests := []struct {
		name              string
		allowedProperties map[string]bool
		src               string
		expectedErr       string
	}{
		{
			name: "invalid key",
			allowedProperties: map[string]bool{
				"enabled": true,
				"timeout": true,
			},
			src:         `set_config(config = {"invalid_key": True})`,
			expectedErr: `key "invalid_key" not found in allowed_findings_properties`,
		},
		{
			name: "expected string, got numeric",
			allowedProperties: map[string]bool{
				"val": true,
			},
			src:         `set_config(config = {"val": 42})`,
			expectedErr: `property "val": unsupported starlark type: int, expected str`,
		},
		{
			name:              "no supported properties",
			allowedProperties: map[string]bool{},
			src:               `set_config(config = {"val": "not a list"})`,
			expectedErr:       `set_config: for parameter "config": no properties are supported in allowed_findings_properties field`,
		},
		{
			name:              "none type ",
			allowedProperties: map[string]bool{"key": true},
			src:               `set_config(config = {"key": None})`,
			expectedErr:       `property "key": unsupported starlark type: NoneType, expected str`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotErr error
			setConfig := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				config := findingsPropertyBag{
					allowedProperties: tc.allowedProperties,
				}
				gotErr = starlark.UnpackArgs(b.Name(), args, kwargs, "config", &config)
				return starlark.None, nil
			}

			predeclared := starlark.StringDict{
				"set_config": starlark.NewBuiltin("set_config", setConfig),
			}

			thread := &starlark.Thread{Name: "test-thread"}
			_, err := starlark.ExecFile(thread, "test.star", tc.src, predeclared)
			if err != nil {
				t.Fatalf("ExecFile failed: %v", err)
			}

			if gotErr == nil {
				t.Fatal("Expected validation error, got nil")
			}

			if !strings.Contains(gotErr.Error(), tc.expectedErr) {
				t.Errorf("Expected error to contain %q, but got: %v", tc.expectedErr, gotErr)
			}
		})
	}
}
