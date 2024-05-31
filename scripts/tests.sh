#!/usr/bin/env bash
# Copyright 2023 The Shac Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eu -o pipefail

# Disable cgo as it's not necessary and not all development platforms have the
# necessary headers.
export CGO_ENABLED=0

cd "$(dirname "${BASH_SOURCE[0]}")"
cd ..
REPO_ROOT="$(pwd)"

CIPD_ROOT="$REPO_ROOT/.tools"
if [ ! -d "$CIPD_ROOT" ]; then
  mkdir "$CIPD_ROOT"
fi

# Install Go using CIPD if it's not on $PATH.
if ! command -v "go" > /dev/null; then
  export GOROOT="$CIPD_ROOT/go"
  echo "- Installing Go from CIPD..."
  cipd init -force "$GOROOT"
  cipd install -log-level error -root "$GOROOT" 'infra/3pp/tools/go/${platform}' version:2@1.22.3
  export PATH="$GOROOT/bin:$PATH"
  echo ""
fi

echo "- Testing with coverage"
go test -count=1 -cover ./...

echo ""
echo "- Running 'shac check'"
go run . check -v

# Benchmarks are the slowest step, so run them last in case the user only cares
# about previous steps and wants to ctrl-C.
echo ""
echo "- Benchmarks"
go test -count=1 -benchtime=200ms -bench=. -run=^$ -cpu 1 ./...
