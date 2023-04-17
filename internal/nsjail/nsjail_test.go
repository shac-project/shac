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

package nsjail

import (
	"runtime"
	"testing"
)

func TestPlatforms(t *testing.T) {
	shouldSupport := runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")

	if shouldSupport && len(Exec) == 0 {
		t.Errorf("nsjail should be supported for platform %s/%s", runtime.GOOS, runtime.GOARCH)
	} else if !shouldSupport && len(Exec) != 0 {
		t.Errorf("nsjail should not be supported for platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}
