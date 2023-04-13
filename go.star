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


def gofmt(ctx):
  """Runs gofmt -s on a Go code base.

  Args:
    ctx: A ctx instance.
  """
  for f in ctx.scm.affected_files():
    if not f.endswith(".go"):
      continue
    diff = ctx.os.exec([
      "gofmt",
      "-s",
      "-d",
      f,
    ]).stdout
    if diff:
      ctx.emit.annotation(level="error", filepath=f, message="`gofmt -s` diff:\n%s" % diff)


def gosec(ctx, version = "v2.15.0", level = "error"):
  """Runs gosec on a Go code base.

  See https://github.com/securego/gosec for more details.

  Args:
    ctx: A ctx instance.
    version: gosec version to install. Defaults to a recent version, that will
      be rolled from time to time.
    level: level at which issues should be emitted.
  """
  exe = _go_install(ctx, "github.com/securego/gosec/v2/cmd/gosec", version)
  res = ctx.os.exec([exe, "-fmt=json", "-quiet", "-exclude=G304", "-exclude-dir=.tools", "./..."], raise_on_failure = False)
  if res.retcode:
    # Schema is https://pkg.go.dev/github.com/securego/gosec/v2#ReportInfo
    d = json.decode(res.stdout)
    o = len(ctx.scm.root)+1
    for file, data in d["Golang errors"]:
      ctx.emit.annotation(
          level="error", message=i["error"], filepath=file[o:], line=int(i["line"]),
          col=int(i["column"]))
    for i in d["Issues"]:
      line = i["line"].split("-")[0]
      ctx.emit.annotation(
          level=level, message=i["rule_id"] + ": " + i["details"],
          filepath=i["file"][o:], line=int(line), col=int(i["column"]))


def ineffassign(ctx, version = "v0.0.0-20230107090616-13ace0543b28"):
  """Runs ineffassign on a Go code base.

  See https://github.com/gordonklaus/ineffassign for more details.

  Args:
    ctx: A ctx instance.
    version: ineffassign version to install. Defaults to a recent version, that
      will be rolled from time to time.
  """
  exe = _go_install(ctx, "github.com/gordonklaus/ineffassign", version)
  res = ctx.os.exec([exe, "./..."], raise_on_failure = False)
  # ineffassign's README claims that it emits a retcode of 1 if it returns any
  # findings, but it actually emits a retcode of 3.
  # https://github.com/gordonklaus/ineffassign/blob/4cc7213b9bc8b868b2990c372f6fa057fa88b91c/ineffassign.go#L70
  if res.retcode not in (0, 3):
    ctx.emit.annotation(
      level="error",
      message="unexpected error from ineffassign (retcode %d):\n%s" % (
        res.retcode,
        res.stderr,
      ),
    )

  # ineffassign emits some duplicate lines.
  for line in sorted(set(res.stderr.splitlines())):
    match = ctx.re.match(r"^%s/(.+):(\d+):(\d+): (.+)$" % ctx.scm.root, line)
    ctx.emit.annotation(
      level="error",
      filepath=match.groups[1],
      line=int(match.groups[2]),
      col=int(match.groups[3]),
      message=match.groups[4],
    )


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
  res = ctx.os.exec([
    exe,
    # TODO(olivernewman): For some reason, including tests results in duplicate
    # findings in non-test files.
    "-test=false",
    "-json",
    "./...",
  ])

  # Example output:
  # {
  #   "github.com/foo/bar": {
  #     "shadow": [
  #       {
  #         "posn": "/abs/path/to/project/file.go:123:8",
  #         "message": "declaration of \"err\" shadows declaration at line 123"
  #       }
  #     ]
  #   }
  # }
  output = json.decode(res.stdout)
  findings = []
  for pkg_findings in output.values():
    findings.extend(pkg_findings["shadow"])

  for finding in findings:
    match = ctx.re.match(r"^%s/(.+):(\d+):(\d+)$" % ctx.scm.root, finding["posn"])
    ctx.emit.annotation(
      level="error",
      filepath=match.groups[1],
      line=int(match.groups[2]),
      col=int(match.groups[3]),
      message=finding["message"],
    )


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
