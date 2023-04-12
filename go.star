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
  exe = _go_install(ctx, "github.com/securego/gosec/v2/cmd/gosec", version)
  if ctx.os.exec([exe, "-fmt=golint", "-quiet", "-exclude=G304", "-exclude-dir=.tools", "./..."], raise_on_failure = False).retcode:
    # TODO(maruel): Emits lines.
    ctx.emit.annotation(level="error", message="failed gosec")


def ineffassign(ctx, version = "v0.0.0-20230107090616-13ace0543b28"):
  """Runs ineffassign on a Go code base.

  See https://github.com/gordonklaus/ineffassign for more details.

  Args:
    ctx: A ctx instance.
    version: ineffassign version to install. Defaults to a recent version, that
      will be rolled from time to time.
  """
  exe = _go_install(ctx, "github.com/gordonklaus/ineffassign", version)
  if ctx.os.exec([exe, "./..."], raise_on_failure = False).retcode:
    # TODO(maruel): Emits lines.
    ctx.emit.annotation(level="error", message="failed ineffassign")


def staticcheck(ctx, version = "v0.4.3"):
  """Runs staticcheck on a Go code base.

  See https://github.com/dominikh/go-tools for more details.

  Args:
    ctx: A ctx instance.
    version: staticcheck version to install. Defaults to a recent version, that
    will be rolled from time to time.
  """
  exe = _go_install(ctx, "honnef.co/go/tools/cmd/staticcheck", version)
  if ctx.os.exec([exe, "./..."], raise_on_failure = False).retcode:
    # TODO(maruel): Emits lines.
    ctx.emit.annotation(level="error", message="failed staticcheck")


def shadow(ctx, version = "v0.7.0"):
  """Runs go vet -vettool=shadow on a Go code base.

  Args:
    ctx: A ctx instance.
    version: shadow version to install. Defaults to a recent version, that will
      be rolled from time to time.
  """
  exe = _go_install(ctx, "golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow", version)
  if ctx.os.exec([exe, "./..."], raise_on_failure = False).retcode:
    ctx.emit.annotation(level="error", message="failed go vet -vettool=shadow")


def _go_install(ctx, pkg, version):
  tool_name = pkg.split("/")[-1]

  # TODO(olivernewman): Stop using a separate GOPATH for each tool, and instead
  # install the tools sequentially. Multiple concurrent `go install` runs on the
  # same GOPATH results in race conditions.
  gopath = "%s/.tools/gopath/%s" % (ctx.scm.root, tool_name)
  gobin = "%s/bin" % gopath
  ctx.os.exec(
    ["go", "install", "%s@%s" % (pkg, version)],
    env = {
      "GOPATH": gopath,
      "GOBIN": gobin,
    },
  )

  return "%s/%s" % (gobin, tool_name)
