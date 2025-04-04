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

load("common.star", "go_env", "go_install")

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
            level = "error",
            message = "needs formatting",
            filepath = f,
            replacements = [new_contents],
        )

gofmt = shac.check(_gofmt, formatter = True)

def _gosec(ctx, version = "v2.22.3", level = "error", exclude = [
    # shac checks are allowed to run arbitrary subprocesses, so it's common for
    # shac's source code to run non-constant subcommands.
    "G204",
    # shac checks are allowed to read arbitrary files, so it's common for shac's
    # source code to read non-constant files.
    "G304",
]):
    """Runs gosec on a Go code base.

    See https://github.com/securego/gosec for more details.

    Args:
      ctx: A ctx instance.
      version: gosec version to install. Defaults to a recent version, that will
        be rolled from time to time.
      level: level at which issues should be emitted.
    """
    affected_files = set(ctx.scm.affected_files())
    exe = go_install(ctx, "github.com/securego/gosec/v2/cmd/gosec", version)
    res = ctx.os.exec(
        [exe, "-fmt=json", "-quiet", "-exclude=%s" % ",".join(exclude), "-exclude-dir=.tools", "-exclude-dir=internal/engine/testdata", "./..."],
        ok_retcodes = (0, 1),
        env = go_env(ctx),
    ).wait()
    if not res.retcode:
        return

    # Schema is https://pkg.go.dev/github.com/securego/gosec/v2#ReportInfo
    d = json.decode(res.stdout)
    o = len(ctx.scm.root) + 1
    for file, errs in d["Golang errors"].items():
        filepath = file[o:]
        if filepath not in affected_files:
            continue
        for e in errs:
            ctx.emit.finding(
                level = "error",
                message = e["error"],
                filepath = filepath,
                line = int(e["line"]),
                col = int(e["column"]),
            )
    for i in d["Issues"]:
        line = i["line"].split("-")[0]
        filepath = i["file"][o:]
        if filepath not in affected_files:
            continue
        ctx.emit.finding(
            level = level,
            message = i["rule_id"] + ": " + i["details"],
            filepath = filepath,
            line = int(line),
            col = int(i["column"]),
        )

gosec = shac.check(_gosec)

def _ineffassign(ctx, version = "v0.0.0-20230107090616-13ace0543b28"):
    """Runs ineffassign on a Go code base.

    See https://github.com/gordonklaus/ineffassign for more details.

    Args:
      ctx: A ctx instance.
      version: ineffassign version to install. Defaults to a recent version, that
        will be rolled from time to time.
    """
    exe = go_install(ctx, "github.com/gordonklaus/ineffassign", version)
    res = ctx.os.exec(
        [exe, "./..."],
        env = go_env(ctx),
        # ineffassign's README claims that it emits a retcode of 1 if it returns any
        # findings, but it actually emits a retcode of 3.
        # https://github.com/gordonklaus/ineffassign/blob/4cc7213b9bc8b868b2990c372f6fa057fa88b91c/ineffassign.go#L70
        ok_retcodes = [0, 3],
    ).wait()

    # ineffassign emits some duplicate lines.
    for line in sorted(set(res.stderr.splitlines())):
        match = ctx.re.match(r"^%s/(.+):(\d+):(\d+): (.+)$" % ctx.scm.root, line)
        ctx.emit.finding(
            level = "error",
            filepath = match.groups[1],
            line = int(match.groups[2]),
            col = int(match.groups[3]),
            message = match.groups[4],
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
    exe = go_install(ctx, "honnef.co/go/tools/cmd/staticcheck", version)
    env = go_env(ctx)
    res = ctx.os.exec(
        [exe, "-f=json", "./..."],
        ok_retcodes = [0, 1],
        env = env,
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
            level = f["severity"],
            message = f["message"],
            # TODO(olivernewman): Add a relpath method for converting abs paths to
            # relative.
            filepath = f["location"]["file"][len(ctx.scm.root) + 1:],
            line = f["location"]["line"],
            col = f["location"]["column"],
            **end_kwargs
        )

staticcheck = shac.check(_staticcheck)

def _shadow(ctx, version = "v0.31.0"):
    """Runs go vet -vettool=shadow on a Go code base.

    Args:
      ctx: A ctx instance.
      version: shadow version to install. Defaults to a recent version, that will
        be rolled from time to time.
    """
    exe = go_install(ctx, "golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow", version)
    res = ctx.os.exec(
        [
            exe,
            # TODO(olivernewman): For some reason, including tests results in
            # duplicate findings in non-test files.
            "-test=false",
            "-json",
            "./...",
        ],
        env = go_env(ctx),
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
            level = "error",
            filepath = match.groups[1],
            line = int(match.groups[2]),
            col = int(match.groups[3]),
            message = finding["message"],
        )

shadow = shac.check(_shadow)

def no_fork_without_lock(ctx):
    """Checks that exec.Command Start() and Run() aren't called directly.

    Instead, callers should use the `execsupport` package, which provides appropriate
    locks to make sure forks are safe.

    Args:
      ctx: A ctx instance.
    """
    output = json.decode(ctx.os.exec(
        [
            "go",
            "run",
            "./internal/go_checks/fork_check",
            "-test=false",
            "-json",
            "./...",
        ],
        env = go_env(ctx),
    ).wait().stdout)

    # Skip the "execsupport" package since it contains the wrappers around Run()
    # and Start() that should be used. But if it's not present in the output,
    # that's a
    # good sign that the check is broken.
    if not output.pop("go.fuchsia.dev/shac-project/shac/internal/execsupport", None):
        fail("execsupport package was not found in the output, fork_check may be buggy")

    for checks in output.values():
        for findings in checks.values():
            for finding in findings:
                match = ctx.re.match(r"^%s/(.+):(\d+):(\d+)$" % ctx.scm.root, finding["posn"])
                ctx.emit.finding(
                    level = "error",
                    filepath = match.groups[1],
                    line = int(match.groups[2]),
                    col = int(match.groups[3]),
                    message = finding["message"],
                )

def govet(
        ctx,
        # `go vet` has a lot of overlap with other linters, only include
        # analyzers that aren't enforced by the other linters.
        analyzers = [
            "copylocks",
        ]):
    """Enforces `go vet`.

    Args:
      ctx: A ctx instance.
      analyzers: Names of analyzers to run (run `go tool vet help` to see all
        options).
    """
    output = ctx.os.exec(
        [
            "go",
            "vet",
            "-json",
        ] +
        ["-" + a for a in analyzers] +
        ["./..."],
        env = go_env(ctx),
    ).wait().stderr

    # output is of the form:
    # # pkg1
    # {}
    # # pkg2
    # {
    #   ...
    # }
    findings_by_package = {}
    current_package_lines = []
    lines = output.splitlines()
    for i, line in enumerate(lines):
        if line.startswith(("warning: ", "go: ")):
            # E.g. "warning: GOPATH set to GOROOT () has no effect"
            continue
        if not line.startswith("# "):
            current_package_lines.append(line)
            if i + 1 < len(lines):
                continue
        if current_package_lines:
            findings_by_package.update(json.decode("\n".join(current_package_lines)))
            current_package_lines = []

    for pkg_findings in findings_by_package.values():
        for check_findings in pkg_findings.values():
            for finding in check_findings:
                match = ctx.re.match(r"^%s/(.+):(\d+):(\d+)$" % ctx.scm.root, finding["posn"])
                ctx.emit.finding(
                    level = "error",
                    filepath = match.groups[1],
                    line = int(match.groups[2]),
                    col = int(match.groups[3]),
                    message = finding["message"],
                )
