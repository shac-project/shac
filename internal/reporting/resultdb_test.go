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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	resultpb "go.chromium.org/luci/resultdb/proto/v1"
	sinkpb "go.chromium.org/luci/resultdb/sink/proto/v1"
	"go.fuchsia.dev/shac-project/shac/internal/engine"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestResultDBReporter(t *testing.T) {
	ctx := context.Background()

	var got []*sinkpb.ReportTestResultsRequest
	var mu sync.Mutex

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
		}
		if err := req.Body.Close(); err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
		}
		var res sinkpb.ReportTestResultsRequest
		if err := protojson.Unmarshal(b, &res); err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
		}

		mu.Lock()
		got = append(got, &res)
		mu.Unlock()
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	luciContextPath := filepath.Join(t.TempDir(), "luci_context.json")
	t.Setenv("LUCI_CONTEXT", luciContextPath)
	writeJSON(t, luciContextPath, luciContext{
		ResultDB: resultDB{
			CurrentInvocation: resultDBInvocation{Name: "foo"},
		},
		ResultSink: resultSinkContext{
			AuthToken:      "s3cr3t",
			ResultSinkAddr: strings.TrimPrefix(server.URL, "http://"),
		},
	})

	r := luci{
		basic: basic{out: io.Discard},
		// Use a very high batch wait duration to ensure we *always* batch small
		// numbers of requests together.
		batchWaitDuration: 24 * time.Hour,
	}
	r.init(ctx)

	startTime := time.Now().Add(-time.Minute)
	r.CheckCompleted(
		ctx, "passing-check", startTime, time.Second, engine.Notice, nil)
	r.CheckCompleted(
		ctx, "failing-check", startTime.Add(5*time.Second), 2*time.Second, engine.Error, nil)
	r.CheckCompleted(
		ctx, "crashing-check", startTime.Add(10*time.Second), 3*time.Second, engine.Nothing, fmt.Errorf("some error"))

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	expected := []*sinkpb.ReportTestResultsRequest{
		{
			TestResults: []*sinkpb.TestResult{
				{
					TestId:    "shac/passing-check",
					Status:    resultpb.TestStatus_PASS,
					Expected:  true,
					StartTime: timestamppb.New(startTime),
					Duration:  durationpb.New(time.Second),
				},
				{
					TestId:    "shac/failing-check",
					Status:    resultpb.TestStatus_FAIL,
					StartTime: timestamppb.New(startTime.Add(5 * time.Second)),
					Duration:  durationpb.New(2 * time.Second),
				},
				{
					TestId:        "shac/crashing-check",
					Status:        resultpb.TestStatus_CRASH,
					StartTime:     timestamppb.New(startTime.Add(10 * time.Second)),
					Duration:      durationpb.New(3 * time.Second),
					FailureReason: &resultpb.FailureReason{PrimaryErrorMessage: "some error"},
				},
			},
		},
	}
	if diff := cmp.Diff(expected, got, protocmp.Transform()); diff != "" {
		t.Errorf("Unexpected requests (-want +got):\n%s", diff)
	}
}

func writeJSON(t *testing.T, path string, obj any) {
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
