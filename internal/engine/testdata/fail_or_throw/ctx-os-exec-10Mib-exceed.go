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

//go:build ignore

package main

func main() {
	// Each line is 1024 bytes, including \n.
	line16 := "0123456789abcdef"
	line64 := line16 + line16 + line16 + line16
	line256 := line64 + line64 + line64 + line64
	line1024 := line256 + line256 + line256 + line256[:len(line256)-1] + "\n"
	for i := 0; i < 10*1024; i++ {
		print(line1024)
	}

	// Print one more byte.
	print("1")
}
