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


def _gofmt(ctx, simplify = True):
  """Runs gofmt on a Go code base.

  Args:
    ctx: A ctx instance.
    simplify: Whether to set the -s flag on gofmt.
  """
  go_files = [f for f in ctx.scm.affected_files() if f.endswith(".go")]
  if not go_files:
    return

  base_cmd = ["gofmt"]
  if simplify:
    base_cmd.append("-s")

  unformatted = ctx.os.exec(base_cmd + ["-l"] + go_files).wait().stdout.splitlines()
  for f in unformatted:
    new_contents = ctx.os.exec(base_cmd + [f]).wait().stdout
    ctx.emit.finding(
      level="error",
      message="needs formatting",
      filepath=f,
      replacements=[new_contents],
    )


gofmt = shac.check(_gofmt, formatter = True)


def _gosec(ctx, version = "v2.15.0", level = "error"):
  """Runs gosec on a Go code base.

  See https://github.com/securego/gosec for more details.

  Args:
    ctx: A ctx instance.
    version: gosec version to install. Defaults to a recent version, that will
      be rolled from time to time.
    level: level at which issues should be emitted.
  """
  exe = _go_install(ctx, "github.com/securego/gosec/v2/cmd/gosec", version)
  res = ctx.os.exec([exe, "-fmt=json", "-quiet", "-exclude=G304", "-exclude-dir=.tools", "./..."], raise_on_failure = False).wait()
  if res.retcode:
    # Schema is https://pkg.go.dev/github.com/securego/gosec/v2#ReportInfo
    d = json.decode(res.stdout)
    o = len(ctx.scm.root)+1
    for file, data in d["Golang errors"]:
      ctx.emit.finding(
          level="error", message=i["error"], filepath=file[o:], line=int(i["line"]),
          col=int(i["column"]))
    for i in d["Issues"]:
      line = i["line"].split("-")[0]
      ctx.emit.finding(
          level=level, message=i["rule_id"] + ": " + i["details"],
          filepath=i["file"][o:], line=int(line), col=int(i["column"]))


gosec = shac.check(_gosec)


def _ineffassign(ctx, version = "v0.0.0-20230107090616-13ace0543b28"):
  """Runs ineffassign on a Go code base.

  See https://github.com/gordonklaus/ineffassign for more details.

  Args:
    ctx: A ctx instance.
    version: ineffassign version to install. Defaults to a recent version, that
      will be rolled from time to time.
  """
  exe = _go_install(ctx, "github.com/gordonklaus/ineffassign", version)
  res = ctx.os.exec(
    [exe, "./..."],
    env = _go_env(ctx, "ineffassign"),
    # ineffassign's README claims that it emits a retcode of 1 if it returns any
    # findings, but it actually emits a retcode of 3.
    # https://github.com/gordonklaus/ineffassign/blob/4cc7213b9bc8b868b2990c372f6fa057fa88b91c/ineffassign.go#L70
    ok_retcodes = [0, 3],
  ).wait()

  # ineffassign emits some duplicate lines.
  for line in sorted(set(res.stderr.splitlines())):
    match = ctx.re.match(r"^%s/(.+):(\d+):(\d+): (.+)$" % ctx.scm.root, line)
    ctx.emit.finding(
      level="error",
      filepath=match.groups[1],
      line=int(match.groups[2]),
      col=int(match.groups[3]),
      message=match.groups[4],
    )


ineffassign = shac.check(_ineffassign)


def _staticcheck(ctx, version = "v0.4.3"):
  """Runs staticcheck on a Go code base.

  See https://github.com/dominikh/go-tools for more details.

  Args:
    ctx: A ctx instance.
    version: staticcheck version to install. Defaults to a recent version, that
    will be rolled from time to time.
  """
  exe = _go_install(ctx, "honnef.co/go/tools/cmd/staticcheck", version)
  env = _go_env(ctx, "staticcheck")
  env["STATICCHECK_CACHE"] = env["GOCACHE"]
  res = ctx.os.exec(
    [exe, "-f=json", "./..."],
    ok_retcodes = [0, 1],
    env = env,
    # TODO(olivernewman): Figure out why staticcheck needs network access and
    # remove. We may need to make sure to `go get` all dependencies first?
    allow_network = True,
  ).wait()

  # Output is JSON-lines.
  # https://staticcheck.io/docs/running-staticcheck/cli/formatters/#json
  for line in res.stdout.splitlines():
    f = json.decode(line)

    end_kwargs = {}
    if f["end"]["line"]:
      end_kwargs["end_line"] = f["end"]["line"]
      end_kwargs["end_col"] = f["end"]["column"]

    ctx.emit.finding(
      # Either "error" or "warning".
      level=f["severity"],
      message=f["message"],
      # TODO(olivernewman): Add a relpath method for converting abs paths to
      # relative.
      filepath=f["location"]["file"][len(ctx.scm.root)+1:],
      line=f["location"]["line"],
      col=f["location"]["column"],
      **end_kwargs
    )


staticcheck = shac.check(_staticcheck)


def _shadow(ctx, version = "v0.7.0"):
  """Runs go vet -vettool=shadow on a Go code base.

  Args:
    ctx: A ctx instance.
    version: shadow version to install. Defaults to a recent version, that will
      be rolled from time to time.
  """
  exe = _go_install(ctx, "golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow", version)
  res = ctx.os.exec(
    [
      exe,
      # TODO(olivernewman): For some reason, including tests results in
      # duplicate findings in non-test files.
      "-test=false",
      "-json",
      "./...",
    ],
    env=_go_env(ctx, "shadow"),
    allow_network=True,
  ).wait()

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
    ctx.emit.finding(
      level="error",
      filepath=match.groups[1],
      line=int(match.groups[2]),
      col=int(match.groups[3]),
      message=finding["message"],
    )


shadow = shac.check(_shadow)


def _go_install(ctx, pkg, version):
  tool_name = pkg.split("/")[-1]
  env = _go_env(ctx, tool_name)
  ctx.os.exec(
    ["go", "install", "%s@%s" % (pkg, version)],
    allow_network = True,
    env = _go_env(ctx, tool_name)
  ).wait()

  return "%s/%s" % (env["GOBIN"], tool_name)


def _go_env(ctx, key):
  # TODO(olivernewman): Stop using a separate GOPATH for each tool, and instead
  # install the tools sequentially. Multiple concurrent `go install` runs on the
  # same GOPATH results in race conditions.
  gopath = "%s/.tools/gopath/%s" % (ctx.scm.root, key)
  return {
    # Disable cgo as it's not necessary and not all development platforms have
    # the necessary headers.
    "CGO_ENABLED": "0",
    "GOFLAGS": " ".join([
      # Disable embedding VCS information because it causes ineffassign builds
      # to fail on some machines.
      "-buildvcs=false",
    ]),
    "GOPATH": gopath,
    "GOBIN": "%s/bin" % gopath,
    # Cache within the directory to avoid writing to $HOME/.cache.
    # TODO(olivernewman): Implement named caches.
    "GOCACHE": "%s/.tools/gocache" % ctx.scm.root,
    # TODO(olivernewman): The default gopackagesdriver is broken within an
    # nsjail.
    "GOPACKAGESDRIVER": "off",
  }
