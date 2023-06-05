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
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	resultpb "go.chromium.org/luci/resultdb/proto/v1"
	sinkpb "go.chromium.org/luci/resultdb/sink/proto/v1"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type luci struct {
	basic
	doneChecks chan *sinkpb.TestResult
	// batchWaitDuration is the duration after having a test result enqueued
	// that the reporter should wait before uploading it to ResultDB, in case
	// more results are reported soon after that can be batched together.
	// Overrideable to allow better determinism during tests.
	batchWaitDuration time.Duration

	mu         sync.Mutex
	wg         sync.WaitGroup
	liveChecks map[string]*sinkpb.TestResult
}

func (l *luci) init(ctx context.Context) error {
	l.doneChecks = make(chan *sinkpb.TestResult)
	l.liveChecks = map[string]*sinkpb.TestResult{}
	r, err := resultSinkCtx()
	if err != nil {
		return err
	}
	// Do uploads in a persistent goroutine so HTTP requests don't block checks
	// from running.
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		client := &http.Client{}
		requests := &sinkpb.ReportTestResultsRequest{}
		for {
			res, ok := <-l.doneChecks
			if res == nil || !ok {
				return
			}
			requests.TestResults = append(requests.TestResults, res)
			for loop := true; loop && len(requests.TestResults) < 500; {
				select {
				case res, ok = <-l.doneChecks:
					if res == nil || !ok {
						loop = false
					} else {
						requests.TestResults = append(requests.TestResults, res)
					}
				// Wait a bit in case we get more results that we can upload in
				// the same batch.
				case <-time.After(l.batchWaitDuration):
					loop = false
				}
			}
			b, err := protojson.MarshalOptions{}.Marshal(requests)
			if err != nil {
				panic(err)
			}
			requests.TestResults = requests.TestResults[:0]
			// TODO(maruel): Run the HTTP request asynchronously.
			if err = r.sendData(ctx, client, "ReportTestResults", b); err != nil {
				fmt.Fprintf(os.Stderr, "TODO: implement HTTP retries! %s\n", err)
			}
		}
	}()
	return nil
}

func (l *luci) Close() error {
	close(l.doneChecks)
	// Wait for the upload goroutine to complete before exiting.
	l.wg.Wait()
	return nil
}

func (l *luci) EmitAnnotation(ctx context.Context, check string, level engine.Level, message, root, file string, s engine.Span, replacements []string) error {
	r := l.getTestResult(check)
	check = html.EscapeString(check)
	lev := html.EscapeString(string(level))
	file = html.EscapeString(file)
	message = html.EscapeString(message)
	if file != "" {
		// TODO(maruel): Do not drop span and replacements!
		if s.Start.Line > 0 {
			r.SummaryHtml += fmt.Sprintf("[%s/%s] %s(%d): %s<br>", check, lev, file, s.Start.Line, message)
			return nil
		}
		r.SummaryHtml += fmt.Sprintf("[%s/%s] %s: %s<br>", check, lev, file, message)
		return nil
	}
	r.SummaryHtml += fmt.Sprintf("[%s/%s] %s<br>", check, lev, message)
	return nil
}

func (l *luci) EmitArtifact(ctx context.Context, check, root, file string, content []byte) error {
	r := l.getTestResult(check)
	if content != nil {
		r.Artifacts[file] = &sinkpb.Artifact{
			Body:        &sinkpb.Artifact_Contents{Contents: content},
			ContentType: "text/plain",
		}
	} else {
		r.Artifacts[file] = &sinkpb.Artifact{
			Body:        &sinkpb.Artifact_FilePath{FilePath: filepath.Join(root, file)},
			ContentType: "text/plain",
		}
	}
	return nil
}

func (l *luci) CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, level engine.Level, err error) {
	r := l.getTestResult(check)
	r.StartTime = timestamppb.New(start)
	r.Duration = durationpb.New(d)
	if err != nil {
		r.Status = resultpb.TestStatus_CRASH
		r.FailureReason = &resultpb.FailureReason{PrimaryErrorMessage: err.Error()}
	} else if level == engine.Error {
		r.Status = resultpb.TestStatus_FAIL
	} else {
		r.Status = resultpb.TestStatus_PASS
		r.Expected = true
	}
	// TODO(maruel): Tag r.Tags with "shac".
	l.mu.Lock()
	delete(l.liveChecks, check)
	l.mu.Unlock()
	l.basic.CheckCompleted(ctx, check, start, d, level, err)
	l.doneChecks <- r
}

func (l *luci) getTestResult(check string) *sinkpb.TestResult {
	l.mu.Lock()
	r := l.liveChecks[check]
	if r == nil {
		r = &sinkpb.TestResult{
			TestId:    "shac/" + check,
			Artifacts: map[string]*sinkpb.Artifact{},
		}
		l.liveChecks[check] = r
	}
	l.mu.Unlock()
	return r
}

// Support code.

// luciContext corresponds to the schema of the file identified by the
// LUCI_CONTEXT env var. See
// https://crsrc.org/i/go/src/go.chromium.org/luci/lucictx/sections.proto for
// the whole structure.
type luciContext struct {
	ResultDB   resultDB          `json:"resultdb"`
	ResultSink resultSinkContext `json:"result_sink"`
}

// resultSinkContext holds the result_sink information parsed from LUCI_CONTEXT.
type resultSinkContext struct {
	AuthToken      string `json:"auth_token"`
	ResultSinkAddr string `json:"address"`
}

type resultDB struct {
	CurrentInvocation resultDBInvocation `json:"current_invocation"`
}

type resultDBInvocation struct {
	Name string `json:"name"`
}

func (r *resultSinkContext) sendData(ctx context.Context, client *http.Client, endpoint string, data []byte) error {
	url := fmt.Sprintf("http://%s/prpc/luci.resultsink.v1.Sink/%s", r.ResultSinkAddr, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	// ResultSink HTTP authorization scheme is documented at
	// https://crsrc.org/i/go/src/go.chromium.org/luci/resultdb/sink/proto/v1/sink.proto;l=30
	req.Header.Add("Authorization", "ResultSink "+r.AuthToken)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, resp.Body)
	if err2 := resp.Body.Close(); err == nil {
		err = err2
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ResultDB HTTP Request error: %s (%d)", http.StatusText(resp.StatusCode), resp.StatusCode)
	}
	return err
}

// resultSinkCtx returns the rdb stream port if available.
func resultSinkCtx() (*resultSinkContext, error) {
	b, err := os.ReadFile(os.Getenv("LUCI_CONTEXT"))
	if err != nil {
		return nil, err
	}
	var ctx luciContext
	if err = json.Unmarshal(b, &ctx); err != nil {
		return nil, err
	}
	// We are clearly running inside a LUCI_CONTEXT luciexe environment but rdb
	// stream was not started. Hard fail since it means we need to fix the recipe.
	if ctx.ResultDB.CurrentInvocation.Name != "" && (ctx.ResultSink.AuthToken == "" || ctx.ResultSink.ResultSinkAddr == "") {
		return nil, fmt.Errorf("resultdb is enabled but not resultsink for invocation %s. Make sure shac is run under \"rdb stream\"", ctx.ResultDB.CurrentInvocation.Name)
	}
	return &ctx.ResultSink, nil
}
