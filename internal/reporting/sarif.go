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
	"io"
	"sort"
	"sync"
	"time"

	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"go.fuchsia.dev/shac-project/shac/internal/sarif"
	"google.golang.org/protobuf/encoding/protojson"
)

// SarifReport converts findings into SARIF JSON output.
type SarifReport struct {
	// SARIF output gets written here when Close() is called.
	Out io.Writer

	mu             sync.Mutex
	resultsByCheck map[string][]*sarif.Result
}

func (sr *SarifReport) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string) error {
	levelMap := map[engine.Level]string{
		engine.Notice:  sarif.Note,
		engine.Warning: sarif.Warning,
		engine.Error:   sarif.Error,
	}
	region := &sarif.Region{
		StartLine:   int32(s.Start.Line), // #nosec G115
		EndLine:     int32(s.End.Line),   // #nosec G115
		StartColumn: int32(s.Start.Col),  // #nosec G115
		EndColumn:   int32(s.End.Col),    // #nosec G115
	}

	var fixes []*sarif.Fix
	for _, repl := range replacements {
		fixes = append(fixes, &sarif.Fix{
			ArtifactChanges: []*sarif.ArtifactChange{
				{
					ArtifactLocation: &sarif.ArtifactLocation{Uri: file},
					Replacements: []*sarif.Replacement{
						{
							DeletedRegion:   region,
							InsertedContent: &sarif.ArtifactContent{Text: repl},
						},
					},
				},
			},
		})
	}

	result := &sarif.Result{
		// TODO(olivernewman): Set RuleId field. The SARIF specification states
		// that ruleId "SHALL" be set, and "Not all existing analysis tools emit
		// the equivalent of a ruleId in their output. A SARIF converter which
		// converts the output of such an analysis tool to the SARIF format
		// SHOULD synthesize ruleId from other information available in the
		// analysis tool's output."
		// https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html#_Toc34317643
		Level:   levelMap[level],
		Message: &sarif.Message{Text: message},
		Locations: []*sarif.Location{
			{
				PhysicalLocation: &sarif.PhysicalLocation{
					ArtifactLocation: &sarif.ArtifactLocation{Uri: file},
					Region:           region,
				},
			},
		},
		Fixes: fixes,
	}

	sr.mu.Lock()
	if sr.resultsByCheck == nil {
		sr.resultsByCheck = make(map[string][]*sarif.Result)
	}
	sr.resultsByCheck[check] = append(sr.resultsByCheck[check], result)
	sr.mu.Unlock()

	return nil
}

func (sr *SarifReport) EmitArtifact(ctx context.Context, root, check, file string, content []byte) error {
	// TODO(olivernewman): Emit artifacts via the `artifacts` SARIF property:
	// https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html#_Toc34317499
	return nil
}

func (sr *SarifReport) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level engine.Level, err error) {
}

func (sr *SarifReport) Print(context.Context, string, string, int, string) {}

func (sr *SarifReport) Close() error {
	doc := &sarif.Document{Version: sarif.Version}
	// Sort for determinism.
	var sortedChecks []string
	for check := range sr.resultsByCheck {
		sortedChecks = append(sortedChecks, check)
	}
	sort.Strings(sortedChecks)

	for _, check := range sortedChecks {
		results := sr.resultsByCheck[check]
		doc.Runs = append(doc.Runs, &sarif.Run{
			Tool: &sarif.Tool{
				Driver: &sarif.ToolComponent{Name: check},
			},
			Results: results,
		})
	}

	b, err := protojson.MarshalOptions{
		Multiline:     true,
		UseProtoNames: false,
	}.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = sr.Out.Write(b)
	return err
}
