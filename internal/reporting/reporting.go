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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

// Report is a closable engine.Report.
//
// Concurrency contract:
//   - Methods may be called concurrently across different check names.
//   - For any single check name, calls to EmitFinding, EmitArtifact, and CheckCompleted
//     are guaranteed to be called sequentially.
type Report interface {
	io.Closer
	engine.Report
}

// Get returns the right reporting implementation based on the current
// environment.
func Get(ctx context.Context) (*MultiReport, error) {
	r := &MultiReport{}

	// On LUCI/Swarming. ResultDB!
	if os.Getenv("LUCI_CONTEXT") != "" {
		l := &luci{
			batchWaitDuration: 20 * time.Millisecond,
		}
		if err := l.init(ctx); err != nil {
			return nil, err
		}
		r.Reporters = append(r.Reporters, l)
	}

	// The following reporters all emit to stdout so they are mutually
	// exclusive.
	switch {
	case os.Getenv("GITHUB_RUN_ID") != "":
		// On GitHub Actions. Emits GitHub Workflows commands.
		r.Reporters = append(r.Reporters, &synchronized{r: &github{out: os.Stdout}})
	case os.Getenv("TERM") != "dumb" && isatty.IsTerminal(os.Stderr.Fd()):
		// Active terminal. Colors! This includes VSCode's integrated terminal.
		r.Reporters = append(r.Reporters, &synchronized{r: &interactive{
			out: colorable.NewColorableStdout(),
		}})
	case os.Getenv("VSCODE_GIT_IPC_HANDLE") != "":
		// VSCode extension.
		// TODO(maruel): Return SARIF.
		r.Reporters = append(r.Reporters, &synchronized{r: &basic{out: os.Stdout}})
	default:
		// Anything else, e.g. redirected output.
		r.Reporters = append(r.Reporters, &synchronized{r: &basic{out: os.Stdout}})
	}

	return r, nil
}

// synchronized wraps a Report object and adds synchronization of calls to
// ensure that checks cannot emit potentially multi-line data simultaneously.
// For example, we don't want two checks to simultaneously emit multi-line
// chunks of output to the command line and have those chunks of output be
// interleaved.
//
// It should be used to wrap any reporter that writes to stdout.
type synchronized struct {
	r  Report
	mu sync.Mutex
}

func (s *synchronized) Close() error {
	return s.r.Close()
}

func (s *synchronized) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, span engine.Span, replacements []string, props map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.EmitFinding(ctx, check, level, message, root, file, span, replacements, props)
}

func (s *synchronized) EmitCommitMessageFinding(ctx context.Context, check string, level engine.Level, message string, commitHash string, commitMessage string, span engine.Span, props map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.EmitCommitMessageFinding(ctx, check, level, message, commitHash, commitMessage, span, props)
}

func (s *synchronized) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.EmitArtifact(ctx, check, root, file, content)
}

func (s *synchronized) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level engine.Level, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.r.CheckCompleted(ctx, check, start, d, level, err)
}

func (s *synchronized) Print(ctx context.Context, check, file string, line int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.r.Print(ctx, check, file, line, message)
}

type basic struct {
	out io.Writer
}

func (b *basic) Close() error {
	return nil
}

func (b *basic) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string, props map[string]string) error {
	_, err := fmt.Fprintln(b.out, overviewString(false, unknownAnsi, check, level, message, root, file, s, replacements, props))
	return err
}

func (b *basic) EmitCommitMessageFinding(ctx context.Context, check string, level engine.Level, message string, commitHash string, commitMessage string, s engine.Span, props map[string]string) error {
	// Use "Commit <hash>" as the file name to reuse standard formatting.
	hashLen := min(len(commitHash), 8)
	_, err := fmt.Fprintln(b.out, overviewString(false, unknownAnsi, check, level, message, "", "Commit "+commitHash[:hashLen], s, nil, nil))
	return err
}

func (b *basic) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	return errors.New("not implemented")
}

func (b *basic) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level engine.Level, err error) {
	if err != nil {
		level = engine.Error
	}
	l := string(level)
	if level == "" || level == engine.Notice {
		l = "success"
	}
	if err != nil {
		fmt.Fprintf(b.out, "- %s (%s in %s): %s\n", check, l, d.Round(time.Millisecond), err)
	} else {
		fmt.Fprintf(b.out, "- %s (%s in %s)\n", check, l, d.Round(time.Millisecond))
	}
}

func (b *basic) Print(ctx context.Context, check, file string, line int, message string) {
	if check != "" {
		fmt.Fprintf(b.out, "- %s [%s:%d] %s\n", check, file, line, message)
	} else {
		fmt.Fprintf(b.out, "[%s:%d] %s\n", file, line, message)
	}
}

// github is the Report implementation when running inside a GitHub Actions
// Workflow.
//
// See https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions
type github struct {
	out io.Writer
}

func (g *github) Close() error {
	return nil
}

func (g *github) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string, props map[string]string) error {
	var builder strings.Builder
	fmt.Fprintf(&builder, "::%s ", level)
	titlePrefix := "::"
	// TODO(eakammer): properties does not have an analog in github actions; however, we could
	// consider including the information in the title or error message
	if file != "" {
		titlePrefix = ","
		fmt.Fprintf(&builder, "::file=%s", file)
		// TODO(maruel): Do not drop replacements!
		if s.Start.Line > 0 {
			fmt.Fprintf(&builder, ",line=%d", s.Start.Line)
			if s.Start.Col > 0 {
				fmt.Fprintf(&builder, ",col=%d", s.Start.Col)
			}
			if s.End.Line > 0 {
				fmt.Fprintf(&builder, ",endLine=%d", s.End.Line)
				if s.End.Col > 0 {
					fmt.Fprintf(&builder, ",endCol=%d", s.End.Col)
				}
			}
		}
	}
	fmt.Fprintf(&builder, "%stitle=%s::%s", titlePrefix, check, message)
	builder.WriteString("\n")
	_, err := fmt.Fprint(g.out, builder.String())
	return err
}

func (g *github) EmitCommitMessageFinding(ctx context.Context, check string, level engine.Level, message string, commitHash string, commitMessage string, s engine.Span, props map[string]string) error {
	var builder strings.Builder
	fmt.Fprintf(&builder, "::%s ", level)
	// GitHub Actions doesn't support commit message annotations directly in a way
	// that ties them to relations chains. Emit as a top-level finding with clarifying text.
	hashLen := min(len(commitHash), 8)
	msg := fmt.Sprintf("Commit %s", commitHash[:hashLen])
	if s.Start.Line > 0 {
		msg += fmt.Sprintf("(%d)", s.Start.Line)
	}
	msg += ": " + message
	fmt.Fprintf(&builder, "::title=%s::%s", check, msg)
	builder.WriteString("\n")
	_, err := fmt.Fprint(g.out, builder.String())
	return err
}

func (g *github) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	return errors.New("not implemented")
}

func (g *github) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, l engine.Level, err error) {
}

func (g *github) Print(ctx context.Context, check, file string, line int, message string) {
	// Use debug here instead of notice since the file/line reference comes from
	// starlark, which will likely not be in the delta or even in your source
	// tree for load()'ed packages. This means GitHub may not be able to
	// reference it anyway.
	if check != "" {
		fmt.Fprintf(g.out, "::debug::%s [%s:%d] %s\n", check, file, line, message)
	} else {
		fmt.Fprintf(g.out, "::debug::[%s:%d] %s\n", file, line, message)
	}
}

type interactive struct {
	out io.Writer
}

func (i *interactive) Close() error {
	return nil
}

func overviewString(withColor bool, color ansiCode, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string, props map[string]string) string {
	var builder strings.Builder
	if withColor {
		fmt.Fprintf(&builder, "%s[%s%s%s/%s%s%s] ", reset, fgHiCyan, check, reset, color, level, reset)
	} else {
		fmt.Fprintf(&builder, "[%s/%s] ", check, level)
	}
	if file != "" {
		builder.WriteString(file)
		if s.Start.Line > 0 {
			fmt.Fprintf(&builder, "(%d)", s.Start.Line)
		}
		builder.WriteString(": ")
	}
	builder.WriteString(message)
	// TODO(maruel): Do not drop replacements!
	if props != nil {
		// create a string for each property then sort them to keep this deterministic
		propStrs := make([]string, 0, len(props))
		for k, v := range props {
			propStrs = append(propStrs, fmt.Sprintf("%q:%v", k, v))
		}
		slices.Sort(propStrs)

		builder.WriteString(" properties: {")
		builder.WriteString(strings.Join(propStrs, ", "))
		builder.WriteString("}")
	}
	return builder.String()
}

func (i *interactive) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string, props map[string]string) error {
	c := levelColor[level]
	_, err := fmt.Fprintln(i.out, overviewString(true, c, check, level, message, root, file, s, replacements, props))
	if err != nil {
		return err
	}
	// If there is no file or start line we can fast exit
	if file == "" || s.Start.Line <= 0 {
		return nil
	}
	// Emit the line and a bit of context in interactive mode.
	b, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return err
	}
	lines := bytes.Split(b, []byte("\n"))
	return i.printHighlightedLines(lines, s, c)
}

func (i *interactive) printHighlightedLines(lines [][]byte, s engine.Span, c ansiCode) error {
	end := s.End.Line
	if end == 0 {
		end = s.Start.Line
	}
	if s.Start.Line >= len(lines) {
		// Consider raising an alert so the check can be fixed.
		return nil
	}
	fmt.Fprintf(i.out, "\n")
	for l := s.Start.Line - 2; l <= end && l < len(lines); l++ {
		if l < 0 {
			continue
		}
		if l == s.Start.Line-1 {
			// First highlighted line.
			if s.Start.Col > 0 {
				if s.End.Line == s.Start.Line && s.End.Col > 0 {
					// Silently ignore when the ending offset is misaligned. It's easy to get wrong.
					// Consider raising an alert so the check can be fixed.
					ec := min(s.End.Col, len(lines[l])+1)
					// Intra-line highlight.
					fmt.Fprintf(i.out, "  %s%s%s%s%s\n", lines[l][:s.Start.Col-1], c, lines[l][s.Start.Col-1:ec-1], reset, lines[l][ec-1:])
				} else {
					fmt.Fprintf(i.out, "  %s%s%s%s\n", lines[l][:s.Start.Col-1], c, lines[l][s.Start.Col-1:], reset)
				}
			} else {
				fmt.Fprintf(i.out, "  %s%s%s\n", c, lines[l], reset)
			}
		} else if l > s.Start.Line-1 && l < end-1 {
			// Middle lines.
			fmt.Fprintf(i.out, "  %s%s%s\n", c, lines[l], reset)
		} else if l >= s.Start.Line && l == end-1 {
			// Last highlighted line.
			if s.End.Col > 0 {
				// Silently ignore when the ending offset is misaligned. It's easy to get wrong.
				// Consider raising an alert so the check can be fixed.
				ec := min(s.End.Col, len(lines[l])+1)
				fmt.Fprintf(i.out, "  %s%s%s%s\n", c, lines[l][:ec-1], reset, lines[l][ec-1:])
			} else {
				fmt.Fprintf(i.out, "  %s%s%s\n", c, lines[l], reset)
			}
		} else {
			fmt.Fprintf(i.out, "  %s\n", lines[l])
		}
	}
	_, err := fmt.Fprintf(i.out, "\n")
	return err
}

func (i *interactive) EmitCommitMessageFinding(ctx context.Context, check string, level engine.Level, message string, commitHash string, commitMessage string, s engine.Span, props map[string]string) error {
	c := levelColor[level]
	// Use "Commit <hash>" as the file name to reuse standard formatting.
	hashLen := min(len(commitHash), 8)
	_, err := fmt.Fprintln(i.out, overviewString(true, c, check, level, message, "", "Commit "+commitHash[:hashLen], s, nil, nil))
	if err != nil {
		return err
	}
	if s.Start.Line <= 0 {
		return nil
	}
	lines := bytes.Split([]byte(commitMessage), []byte("\n"))
	return i.printHighlightedLines(lines, s, c)
}

func (i *interactive) EmitArtifact(ctx context.Context, root, check, file string, content []byte) error {
	return errors.New("not implemented")
}

func (i *interactive) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level engine.Level, err error) {
	if err != nil {
		level = engine.Error
	}
	c := levelColor[level]
	l := string(level)
	if level == "" || level == engine.Notice {
		l = "success"
	}
	if err != nil {
		fmt.Fprintf(i.out, "%s- %s%s%s (%s in %s): %s\n", reset, c, check, reset, l, d.Round(time.Millisecond), err)
	} else {
		fmt.Fprintf(i.out, "%s- %s%s%s (%s in %s)\n", reset, c, check, reset, l, d.Round(time.Millisecond))
	}
}

func (i *interactive) Print(ctx context.Context, check, file string, line int, message string) {
	if check != "" {
		fmt.Fprintf(i.out, "%s- %s%s %s[%s%s:%d%s] %s%s%s\n", reset, fgYellow, check, reset, fgHiBlue, file, line, reset, bold, message, reset)
	} else {
		fmt.Fprintf(i.out, "%s[%s%s:%d%s] %s%s%s\n", reset, fgHiBlue, file, line, reset, bold, message, reset)
	}
}

var levelColor = map[engine.Level]ansiCode{
	engine.Notice:  fgGreen,
	engine.Warning: fgYellow,
	engine.Error:   fgRed,
	engine.Nothing: fgGreen,
}
