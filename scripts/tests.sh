#!/usr/bin/env bash
# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -eu -o pipefail

REPO_ROOT="$(realpath "$(dirname "$(dirname "${BASH_SOURCE[0]}")")")"
cd "$REPO_ROOT"

CIPD_ROOT="$REPO_ROOT/.tools"
if [ ! -d "$CIPD_ROOT" ]; then
  mkdir "$CIPD_ROOT"
fi
# Make it so "go install" installs locally.
export GOPATH="$CIPD_ROOT"
export GOBIN="$CIPD_ROOT/bin"
export PATH="$CIPD_ROOT/bin:$PATH"

# Install Go using CIPD if it's not on $PATH.
if ! command -v "go" > /dev/null; then
  export GOROOT="$CIPD_ROOT/go"
  echo "- Installing Go from CIPD..."
  cipd init -force "$GOROOT"
  cipd install -log-level error -root "$GOROOT" 'infra/3pp/tools/go/${platform}'
  export PATH="$GOROOT/bin:$PATH"
fi

echo "- Testing"
go test -cover ./...

echo "- Running"
go run . check -v
