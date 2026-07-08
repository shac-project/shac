#!/usr/bin/env bash
# Copyright 2026 The Shac Authors
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

cd "$(dirname "${BASH_SOURCE[0]}")"
cd ..
REPO_ROOT="$(pwd)"

CIPD_ROOT="$REPO_ROOT/.tools"
if [ ! -d "$CIPD_ROOT" ]; then
  mkdir "$CIPD_ROOT"
fi

cipd init -force "$CIPD_ROOT"
cipd install \
  -log-level error \
  -root ${CIPD_ROOT} \
  'infra/3pp/tools/protoc/${platform}'
export PATH="$CIPD_ROOT/bin:$PATH"

# Use whatever toolchain is locally installed, rather than trying to download
# the toolchain version specified in go.mod.
export GOTOOLCHAIN=local

# LINT.IfChange(goversion)
GO_CIPD_VERSION="version:3@1.26.1"
# LINT.ThenChange(/go.mod:goversion)

# Install Go using CIPD if it's not on $PATH.
if ! command -v "go" > /dev/null; then
  export GOROOT="$CIPD_ROOT/go"
  echo "- Installing Go from CIPD..."
  cipd init -force "$GOROOT"
  cipd install \
    -log-level error \
    -root "$GOROOT" \
    'infra/3pp/tools/go/${platform}' \
    "$GO_CIPD_VERSION"
  export PATH="$GOROOT/bin:$PATH"
  echo ""
fi

go generate ./...
