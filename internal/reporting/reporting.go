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
	"time"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
)

// Report is a closable engine.Report.
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
		r.Reporters = append(r.Reporters, &github{out: os.Stdout})
	case os.Getenv("TERM") != "dumb" && isatty.IsTerminal(os.Stderr.Fd()):
		// Active terminal. Colors! This includes VSCode's integrated terminal.
		r.Reporters = append(r.Reporters, &interactive{
			out: colorable.NewColorableStdout(),
		})
	case os.Getenv("VSCODE_GIT_IPC_HANDLE") != "":
		// VSCode extension.
		// TODO(maruel): Return SARIF.
		r.Reporters = append(r.Reporters, &basic{out: os.Stdout})
	default:
		// Anything else, e.g. redirected output.
		r.Reporters = append(r.Reporters, &basic{out: os.Stdout})
	}

	return r, nil
}

type basic struct {
	out io.Writer
}

func (b *basic) Close() error {
	return nil
}

func (b *basic) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string) error {
	if file != "" {
		// TODO(maruel): Do not drop span and replacements!
		if s.Start.Line > 0 {
			_, err := fmt.Fprintf(b.out, "[%s/%s] %s(%d): %s\n", check, level, file, s.Start.Line, message)
			return err
		}
		_, err := fmt.Fprintf(b.out, "[%s/%s] %s: %s\n", check, level, file, message)
		return err
	}
	_, err := fmt.Fprintf(b.out, "[%s/%s] %s\n", check, level, message)
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

func (g *github) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string) error {
	if file != "" {
		// TODO(maruel): Do not drop replacements!
		if s.Start.Line > 0 {
			if s.End.Line > 0 {
				if s.End.Col > 0 {
					_, err := fmt.Fprintf(g.out, "::%s ::file=%s,line=%d,col=%d,endLine=%d,endCol=%d,title=%s::%s\n",
						level, file, s.Start.Line, s.Start.Col, s.End.Line, s.End.Col, check, message)
					return err
				}
				_, err := fmt.Fprintf(g.out, "::%s ::file=%s,line=%d,endLine=%d,title=%s::%s\n",
					level, file, s.Start.Line, s.End.Line, check, message)
				return err
			}
			if s.Start.Col > 0 {
				_, err := fmt.Fprintf(g.out, "::%s ::file=%s,line=%d,col=%d,title=%s::%s\n",
					level, file, s.Start.Line, s.Start.Col, check, message)
				return err
			}
			_, err := fmt.Fprintf(g.out, "::%s ::file=%s,line=%d,title=%s::%s\n",
				level, file, s.Start.Line, check, message)
			return err
		}
		_, err := fmt.Fprintf(g.out, "::%s ::file=%stitle=%s::%s\n", level, file, check, message)
		return err
	}
	_, err := fmt.Fprintf(g.out, "::%s ::title=%s::%s\n", level, check, message)
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

func (i *interactive) EmitFinding(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string) error {
	c := levelColor[level]
	if file != "" {
		// TODO(maruel): Do not drop replacements!
		if s.Start.Line > 0 {
			fmt.Fprintf(i.out, "%s[%s%s%s/%s%s%s] %s(%d): %s\n", reset, fgHiCyan, check, reset, c, level, reset, file, s.Start.Line, message)

			// Emit the line and a bit of context in interactive mode.
			b, err := os.ReadFile(filepath.Join(root, file))
			if err != nil {
				return err
			}
			lines := bytes.Split(b, []byte("\n"))
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
							ec := s.End.Col
							if ec > len(lines[l])+1 {
								// Consider raising an alert so the check can be fixed.
								ec = len(lines[l]) + 1
							}
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
						ec := s.End.Col
						if ec > len(lines[l])+1 {
							// Consider raising an alert so the check can be fixed.
							ec = len(lines[l]) + 1
						}
						fmt.Fprintf(i.out, "  %s%s%s%s\n", c, lines[l][:ec-1], reset, lines[l][ec-1:])
					} else {
						fmt.Fprintf(i.out, "  %s%s%s\n", c, lines[l], reset)
					}
				} else {
					fmt.Fprintf(i.out, "  %s\n", lines[l])
				}
			}
			_, err = fmt.Fprintf(i.out, "\n")
			return err
		}
		_, err := fmt.Fprintf(i.out, "%s[%s%s%s/%s%s%s] %s: %s\n", reset, fgHiCyan, check, reset, c, level, reset, file, message)
		return err
	}
	_, err := fmt.Fprintf(i.out, "%s[%s%s%s/%s%s%s] %s\n", reset, fgHiCyan, check, reset, c, level, reset, message)
	return err
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
