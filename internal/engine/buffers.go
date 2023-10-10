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
	"log"
	"sync"
)

// buffers is the shared buffers across all parallel checks.
//
// Fill up 3 large buffers to accelerate the bootstrap.
var buffers = buffersImpl{
	b: map[*bytes.Buffer]struct{}{
		bytes.NewBuffer(make([]byte, 0, 16*1024)): {},
		bytes.NewBuffer(make([]byte, 0, 16*1024)): {},
		bytes.NewBuffer(make([]byte, 0, 16*1024)): {},
	},
}

type buffersImpl struct {
	mu sync.Mutex
	// Track buffers in a map to prevent storing duplicates in the pool.
	b map[*bytes.Buffer]struct{}
}

func (i *buffersImpl) get() *bytes.Buffer {
	var b *bytes.Buffer
	i.mu.Lock()
	if len(i.b) == 0 {
		b = &bytes.Buffer{}
	} else {
		// Choose a random element from the pool by taking whatever buffer is
		// returned first when iterating over the pool.
		for b = range i.b {
			break
		}
		delete(i.b, b)
	}
	i.mu.Unlock()
	return b
}

func (i *buffersImpl) push(b *bytes.Buffer) {
	// Reset keeps the buffer, so that the next execution will reuse the same allocation.
	b.Reset()
	i.mu.Lock()
	if _, ok := i.b[b]; ok {
		i.mu.Unlock()
		log.Panicf("buffer at %p has already been returned to the pool", b)
	}
	i.b[b] = struct{}{}
	i.mu.Unlock()
}
