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
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
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
		sarifRepl := &sarif.Replacement{
			DeletedRegion:   region,
			InsertedContent: &sarif.ArtifactContent{Text: repl},
		}
		sarifRepls, err := sr.splitReplacement(sarifRepl, root, file)
		if err != nil {
			// Errors here are unexpected, but if one occurs it's fine to just
			// stick with the full-file replacement.
			log.Printf("failed to split full-file replacement for %q: %s", file, err)
			sarifRepls = []*sarif.Replacement{sarifRepl}
		}
		fixes = append(fixes, &sarif.Fix{
			ArtifactChanges: []*sarif.ArtifactChange{
				{
					ArtifactLocation: &sarif.ArtifactLocation{Uri: file},
					Replacements:     sarifRepls,
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

// splitReplacement splits a whole-file replacement into more readable chunks
// using a diffing library.
func (sr *SarifReport) splitReplacement(repl *sarif.Replacement, root, file string) ([]*sarif.Replacement, error) {
	// Only if StartLine==0 (indicating the whole file is being replaced) should
	// we attempt to split the replacement.
	if repl.DeletedRegion.StartLine != 0 || file == "" {
		return []*sarif.Replacement{repl}, nil
	}

	b, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return nil, err
	}

	oldLines := strings.SplitAfter(string(b), "\n")
	newLines := strings.SplitAfter(repl.InsertedContent.Text, "\n")

	return replacementsForDiff(oldLines, newLines), nil
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

// replacementsForDiff takes a diff between oldLines and newLines and converts
// it to corresponding SARIF replacement objects.
func replacementsForDiff(oldLines, newLines []string) []*sarif.Replacement {
	var res []*sarif.Replacement

	// GetGroupedOpCodes returns a list of "operation groups", where each group
	// corresponds to a chunk of adjacent changed lines.
	for _, group := range difflib.NewMatcher(oldLines, newLines).GetGroupedOpCodes(0) {
		// group.I1, group.I2 are oldLines start and end indices.
		// group.J1, group.J2 are newLines start and end indices.
		startLine, endLine := group[0].I1, group[len(group)-1].I2
		var startCol, endCol int32

		if startLine == endLine {
			endLine++
			startCol, endCol = 1, 1
		}

		var lines []string
		for _, op := range group {
			switch op.Tag {
			// e == "equal" (unchanged)
			// i == "inserted"
			// r == "replaced"
			case 'e', 'i', 'r':
				lines = append(lines, newLines[op.J1:op.J2]...)
			// d == "deleted"
			case 'd':
			default:
				log.Panicf("Invalid opcode during diff %s", string(op.Tag))
			}
		}

		res = append(res, &sarif.Replacement{
			DeletedRegion: &sarif.Region{
				// Convert start line from zero-based to one-based.
				StartLine: int32(startLine) + 1, // #nosec:G115
				// Convert end line from zero-based exclusive to one-based
				// inclusive, which ends up being a no-op.
				EndLine:     int32(endLine), // #nosec:G115
				StartColumn: startCol,
				EndColumn:   endCol,
			},
			InsertedContent: &sarif.ArtifactContent{Text: strings.Join(lines, "")},
		})
	}

	return res
}
