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
	"bytes"
	"fmt"
	"testing"
)

func TestBuffers(t *testing.T) {
	t.Parallel()

	t.Run("get buffer from empty pool", func(t *testing.T) {
		t.Parallel()
		// Avoid using the shared global buffer pool, for determinism.
		buffers := buffersImpl{b: make(map[*bytes.Buffer]struct{})}

		b := buffers.get()
		if b.Len() != 0 {
			t.Errorf("Expected new buffer to have length 0, got %d", b.Len())
		}
		if b.Cap() != 0 {
			t.Errorf("Expected new buffer to have capacity 0, got %d", b.Cap())
		}

		_, err := b.WriteString("hello, world")
		if err != nil {
			t.Fatal(err)
		}
		wantCap := b.Cap()

		buffers.push(b)

		b2 := buffers.get()
		if b2 != b {
			t.Errorf("buffers.get() should return the existing buffer")
		}
		if b2.Len() != 0 {
			t.Errorf("Expected reused buffer to be empty, but it contains: %q", b2)
		}
		if b2.Cap() != wantCap {
			t.Errorf("Expected reused buffer to have capacity %d, got %d", wantCap, b2.Cap())
		}
	})

	t.Run("push the same buffer multiple times", func(t *testing.T) {
		t.Parallel()
		// Avoid using the shared global buffer pool, for determinism.
		buffers := buffersImpl{b: make(map[*bytes.Buffer]struct{})}

		b := buffers.get()

		buffers.push(b)

		defer func() {
			msg := recover()
			if msg == nil {
				t.Errorf("Expected a panic")
			}
			want := fmt.Sprintf("buffer at %p has already been returned to the pool", b)
			if msg != want {
				t.Errorf("Got wrong panic message: %s", msg)
			}
		}()
		buffers.push(b)
	})
}
