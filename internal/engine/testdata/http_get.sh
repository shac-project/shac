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

# TODO(olivernewman): Rewrite this script as a Go file that can be run using `go
# run`, which is more portable than depending on wget/curl. `go run` is slow
# because the filesystem sandbox blocks access to $GOCACHE so the entire stdlib
# needs to be recompiled every time the test runs, which should be fixed.

set -eu -o pipefail

url="$1"
timeout_secs="1"
network_unavailable_msg="Network unavailable"

retcode=0
if command -v curl > /dev/null; then
  curl --silent --connect-timeout $timeout_secs "$url" || retcode=$?
  if [ "$retcode" -eq 7 ]; then
    echo "$network_unavailable_msg"
    exit 1
  else
    exit "$retcode"
  fi
elif command -v wget > /dev/null; then
  wget --quiet --output-document=- --timeout=$timeout_secs "$url" || retcode=$?
  if [ "$retcode" -eq 4 ]; then
    echo "$network_unavailable_msg"
    exit 1
  else
    exit "$retcode"
  fi
else
  echo "neither wget nor curl is available"
  exit 1
fi
