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


def gosec(ctx, version = "v2.15.0"):
  """Runs gosec on a Go code base.

  See https://github.com/securego/gosec for more details.

  Args:
    ctx: A ctx instance.
    version: gosec version to install. Defaults to a recent version, that will
      be rolled from time to time.
  """
  # TODO(maruel): Always install locally with GOBIN=.tools
  ctx.os.exec(["go", "install", "github.com/securego/gosec/v2/cmd/gosec@" + version])
  if ctx.os.exec(["gosec", "-fmt=golint", "-quiet", "-exclude=G304", "-exclude-dir=.tools", "./..."], raise_on_failure = False).retcode:
    # TODO(maruel): Emits lines.
    ctx.emit.annotation(level="error", message="failed gosec")


def staticcheck(ctx, version = "v0.4.3"):
  """Runs staticcheck on a Go code base.

  See https://github.com/dominikh/go-tools for more details.

  Args:
    ctx: A ctx instance.
    version: staticcheck version to install. Defaults to a recent version, that
    will be rolled from time to time.
  """
  # TODO(maruel): Always install locally with GOBIN=.tools
  ctx.os.exec(["go", "install", "honnef.co/go/tools/cmd/staticcheck@" + version])
  if ctx.os.exec(["staticcheck", "./..."], raise_on_failure = False).retcode:
    # TODO(maruel): Emits lines.
    ctx.emit.annotation(level="error", message="failed staticcheck")
