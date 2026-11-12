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
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"go.fuchsia.dev/shac-project/shac/internal/sarif"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// SarifReport converts findings into SARIF JSON output.
type SarifReport struct {
	// SARIF output gets written here when Close() is called.
	Out io.Writer

	mu             sync.Mutex
	resultsByCheck map[string][]*sarif.Result
	// regionsWithColumnsByFile holds the regions of file findings that specify
	// columns (which need converting from byte offsets to UTF-16 code units),
	// keyed by absolute file path. Conversion is deferred until Close() so
	// that each file is read only once, and only one file is held in memory
	// at a time.
	regionsWithColumnsByFile map[string][]*sarif.Region
}

var levelMap = map[engine.Level]string{
	engine.Notice:  sarif.Note,
	engine.Warning: sarif.Warning,
	engine.Error:   sarif.Error,
}

func (sr *SarifReport) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string, props map[string]string) error {
	region := spanToRegion(s)

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

	propsProto, err := propsToStructProto(props)
	if err != nil {
		return err
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
		Fixes:      fixes,
		Properties: propsProto,
	}

	sr.mu.Lock()
	if sr.resultsByCheck == nil {
		sr.resultsByCheck = make(map[string][]*sarif.Result)
	}
	sr.resultsByCheck[check] = append(sr.resultsByCheck[check], result)
	if root != "" && file != "" && hasColumns(region) {
		if sr.regionsWithColumnsByFile == nil {
			sr.regionsWithColumnsByFile = make(map[string][]*sarif.Region)
		}
		path := filepath.Join(root, file)
		// Fixes share the location's region, so converting it once covers
		// both.
		sr.regionsWithColumnsByFile[path] = append(sr.regionsWithColumnsByFile[path], region)
	}
	sr.mu.Unlock()

	return nil
}

func (sr *SarifReport) EmitCommitMessageFinding(ctx context.Context, check string, level engine.Level, message string, commitHash string, commitMessage string, s engine.Span, props map[string]string) error {
	region := spanToRegion(s)
	if hasColumns(region) {
		convertColumns(region, bytes.Split([]byte(commitMessage), []byte("\n")))
	}

	propsProto, err := propsToStructProto(props)
	if err != nil {
		return err
	}

	// Attach the commit hash so downstream processors can attribute this finding to the
	// correct commit in a stack.
	locPropsProto, err := structpb.NewStruct(map[string]any{
		"commit": commitHash,
	})
	if err != nil {
		return err
	}

	result := &sarif.Result{
		Level:   levelMap[level],
		Message: &sarif.Message{Text: message},
		Locations: []*sarif.Location{
			{
				PhysicalLocation: &sarif.PhysicalLocation{
					ArtifactLocation: &sarif.ArtifactLocation{
						// Use a placeholder URI recognizable by downstream tools (like Gerrit) as
						// representing the commit message.
						Uri:        "/COMMIT_MSG",
						Properties: locPropsProto,
					},
					Region: region,
				},
			},
		},
		Properties: propsProto,
	}

	sr.mu.Lock()
	if sr.resultsByCheck == nil {
		sr.resultsByCheck = make(map[string][]*sarif.Result)
	}
	sr.resultsByCheck[check] = append(sr.resultsByCheck[check], result)
	sr.mu.Unlock()

	return nil
}

func propsToStructProto(props map[string]string) (*structpb.Struct, error) {
	if len(props) == 0 {
		return nil, nil
	}
	p := make(map[string]any, len(props))
	for k, v := range props {
		p[k] = v
	}
	return structpb.NewStruct(p)
}

// spanToRegion converts an engine.Span into a sarif.Region. The region's
// columns are still 1-based UTF-8 byte offsets, as in the engine.Span, until
// convertColumns is called on it.
func spanToRegion(s engine.Span) *sarif.Region {
	return &sarif.Region{
		StartLine:   int32(s.Start.Line), // #nosec G115
		EndLine:     int32(s.End.Line),   // #nosec G115
		StartColumn: int32(s.Start.Col),  // #nosec G115
		EndColumn:   int32(s.End.Col),    // #nosec G115
	}
}

// hasColumns reports whether r has columns that may change when converted
// from byte offsets to UTF-16 code units. Column 1 is the same in both.
func hasColumns(r *sarif.Region) bool {
	return r.StartColumn > 1 || r.EndColumn > 1
}

// convertColumns converts r's columns from 1-based UTF-8 byte offsets into
// lines to 1-based UTF-16 code unit offsets, matching SARIF's default
// columnKind = "utf16CodeUnits" and downstream consumers like Gerrit.
func convertColumns(r *sarif.Region, lines [][]byte) {
	endLine := r.EndLine
	if endLine == 0 {
		endLine = r.StartLine
	}
	if r.StartColumn > 1 && r.StartLine > 0 && int(r.StartLine) <= len(lines) {
		r.StartColumn = byteColToUTF16Col(lines[r.StartLine-1], r.StartColumn)
	}
	if r.EndColumn > 1 && endLine > 0 && int(endLine) <= len(lines) {
		r.EndColumn = byteColToUTF16Col(lines[endLine-1], r.EndColumn)
	}
}

func byteColToUTF16Col(line []byte, byteCol int32) int32 {
	col := int32(1)
	for b := line[:min(int(byteCol)-1, len(line))]; len(b) > 0; {
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		col += int32(utf16.RuneLen(r)) // #nosec G115
	}
	return col
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
	if err == nil {
		return
	}

	result := &sarif.Result{
		Level:   sarif.Error,
		Message: &sarif.Message{Text: err.Error()},
	}
	if stackerr, ok := errors.AsType[engine.BacktraceableError](err); ok {
		result.Message.Text = err.Error() + "\n\n" + stackerr.Backtrace()
	}

	sr.mu.Lock()
	if sr.resultsByCheck == nil {
		sr.resultsByCheck = make(map[string][]*sarif.Result)
	}
	sr.resultsByCheck[check] = append(sr.resultsByCheck[check], result)
	sr.mu.Unlock()
}

func (sr *SarifReport) Print(context.Context, string, string, int, string) {}

func (sr *SarifReport) Close() error {
	// Findings store columns as UTF-8 byte offsets, but SARIF consumers like
	// Gerrit expect UTF-16 code units, and converting requires the text of
	// each finding's line. Do the conversion here rather than as findings are
	// emitted, so each file is read once no matter how many findings it has,
	// and only one file's contents are held in memory at a time. Sort the
	// paths so that read errors are deterministic.
	paths := slices.Sorted(maps.Keys(sr.regionsWithColumnsByFile))
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			// The file may have been deleted from the checkout (e.g. by the
			// user) since the finding was emitted. Leave the columns
			// unconverted, which are still correct for ASCII lines.
			continue
		} else if err != nil {
			return err
		}
		lines := bytes.Split(b, []byte("\n"))
		for _, r := range sr.regionsWithColumnsByFile[path] {
			convertColumns(r, lines)
		}
	}

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

		if startLine == endLine {
			if startLine < len(oldLines) {
				// Gerrit doesn't cleanly handle 0-length comment ranges that
				// represent pure insertions, so we expand the region to cover the
				// adjacent original line and append the original content to the
				// inserted content.
				lines = append(lines, oldLines[startLine])
			} else {
				startCol, endCol = 1, 1
			}
			endLine++
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
