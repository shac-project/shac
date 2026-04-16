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

module go.fuchsia.dev/shac-project/shac

// LINT.IfChange(goversion)
go 1.26.1

// LINT.ThenChange(scripts/tests.sh:goversion, .github/workflows/test.yml:test_goversion, .github/workflows/test.yml:lint_goversion, .github/workflows/test.yml:codeql_goversion)

require (
	github.com/go-git/go-git/v5 v5.17.2
	github.com/google/go-cmp v0.7.0
	github.com/mattn/go-colorable v0.1.13
	github.com/mattn/go-isatty v0.0.20
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2
	github.com/spf13/pflag v1.0.5
	go.chromium.org/luci v0.0.0-20260416130104-3fc11caf5caa
	go.starlark.net v0.0.0-20250804182900-3c9dc17c5f2e
	golang.org/x/mod v0.33.0
	golang.org/x/sync v0.20.0
	golang.org/x/tools v0.42.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.8.0 // indirect
	github.com/golang/mock v1.7.0-rc.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/julienschmidt/httprouter v1.3.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/grpc v1.80.0 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
)
