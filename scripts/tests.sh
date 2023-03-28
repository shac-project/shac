#!/usr/bin/env bash
# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -eu -o pipefail

REPO_ROOT="$(dirname "$(dirname "${BASH_SOURCE[0]}")")"
cd "$REPO_ROOT"

GO=go

# Install Go using CIPD if it's not on $PATH.
if ! command -v "$GO" > /dev/null; then
  CIPD_ROOT="$REPO_ROOT/.tools"
  if [ ! -d "$CIPD_ROOT" ]; then
    echo "- Installing Go from CIPD..."
    cipd init -force "$CIPD_ROOT"
    cipd install -log-level error -root "$CIPD_ROOT" 'infra/3pp/tools/go/${platform}'
  fi
  GO="$CIPD_ROOT/bin/go"
fi

echo "- Testing"
"$GO" test -cover ./...

echo "- Running"
"$GO" run . check -v
