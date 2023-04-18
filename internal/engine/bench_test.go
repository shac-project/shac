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

import "testing"

func BenchmarkSimple(b *testing.B) {
	b.ReportAllocs()
	want := "[//shac-register_check.star:16] running\n"
	for i := 0; i < b.N; i++ {
		testStarlarkPrint(b, "testdata/print", "shac-register_check.star", true, want)
	}
}

// TODO(maruel): Add benchmark that calls ctx.os.exec() multiple times concurrently.

// TODO(maruel): Add benchmark that calls new_lines() multiple times to ensure cached performance.

// TODO(maruel): Add large synthetic benchmark.
