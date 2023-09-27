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

package reporting

import (
	"context"
	"time"

	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"golang.org/x/sync/errgroup"
)

// MultiReport is a Report that wraps any number of other Report objects and
// tees output to all of them.
type MultiReport struct {
	Reporters []Report
}

var _ Report = (*MultiReport)(nil)

func (t *MultiReport) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string) error {
	return t.do(func(r Report) error {
		return r.EmitFinding(ctx, check, level, message, root, file, s, replacements)
	})
}

func (t *MultiReport) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	return t.do(func(r Report) error {
		return r.EmitArtifact(ctx, check, root, file, content)
	})
}

func (t *MultiReport) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level engine.Level, err error) {
	_ = t.do(func(r Report) error {
		r.CheckCompleted(ctx, check, start, d, level, err)
		return nil
	})
}

func (t *MultiReport) Print(ctx context.Context, check, file string, line int, message string) {
	_ = t.do(func(r Report) error {
		r.Print(ctx, check, file, line, message)
		return nil
	})
}

func (t *MultiReport) Close() error {
	return t.do(func(r Report) error {
		return r.Close()
	})
}

func (t *MultiReport) do(f func(r Report) error) error {
	var eg errgroup.Group
	for _, r := range t.Reporters {
		r := r
		eg.Go(func() error {
			return f(r)
		})
	}
	return eg.Wait()
}
