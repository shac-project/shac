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
	"sync"
)

// buffers is the shared buffers across all parallel checks.
//
// Fill up 3 large buffers to accelerate the bootstrap.
var buffers = buffersImpl{
	b: []*bytes.Buffer{
		bytes.NewBuffer(make([]byte, 0, 16*1024)),
		bytes.NewBuffer(make([]byte, 0, 16*1024)),
		bytes.NewBuffer(make([]byte, 0, 16*1024)),
	},
}

type buffersImpl struct {
	mu sync.Mutex
	b  []*bytes.Buffer
}

func (i *buffersImpl) get() *bytes.Buffer {
	var b *bytes.Buffer
	i.mu.Lock()
	if l := len(i.b); l == 0 {
		b = &bytes.Buffer{}
	} else {
		b = i.b[l-1]
		i.b = i.b[:l-1]
	}
	i.mu.Unlock()
	return b
}

func (i *buffersImpl) push(b *bytes.Buffer) {
	// Reset keeps the buffer, so that the next execution will reuse the same allocation.
	b.Reset()
	i.mu.Lock()
	i.b = append(i.b, b)
	i.mu.Unlock()
}
