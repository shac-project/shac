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
    go_files = ctx.scm.affected_files(glob = "**/*.go")
    if not go_files:
        return

    base_cmd = ["gofmt"]
    if simplify:
        base_cmd.append("-s")

    unformatted = ctx.os.exec(base_cmd + ["-l"] + list(go_files)).wait().stdout.splitlines()
    for f in unformatted:
        new_contents = ctx.os.exec(base_cmd + [f]).wait().stdout
        ctx.emit.finding(
            level = "error",
            message = "needs formatting",
            filepath = f,
            replacements = [new_contents],
        )

gofmt = shac.check(_gofmt, formatter = True)

def _gosec(ctx, version = "v2.29.0", level = "error", exclude = [
    # shac checks are allowed to run arbitrary subprocesses, so it's common for
    # shac's source code to run non-constant subcommands.
    "G204",
    # shac checks are allowed to read arbitrary files, so it's common for shac's
    # source code to read non-constant files.
    "G304",
    # G703 is the taint-analysis counterpart to G304 (path traversal via taint
    # analysis) and similarly flags reading or writing user-specified file
    # paths.
    "G703",
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

def _shadow(ctx, version = "v0.48.0"):
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
            "unusedresult",
        ]):
    """Enforces `go vet`.

    Args:
      ctx: A ctx instance.
      analyzers: Names of analyzers to run (run `go tool vet help` to see all
        options).
    """
    stdout = ctx.os.exec(
        [
            "go",
            "vet",
            "-json",
        ] +
        ["-" + a for a in analyzers] +
        ["./..."],
        env = go_env(ctx),
    ).wait().stdout

    for finding in _parse_analyzer_json(stdout, "go vet"):
        _emit_analyzer_finding(ctx, finding, "go vet")

def _gofix(ctx):
    """Runs the `go fix` analyzers on a Go code base.

    `go fix` runs a suite of analyzers that modernize Go code by rewriting
    outdated idioms to take advantage of newer language features and standard
    library APIs, e.g. replacing `interface{}` with `any`, or a
    `sort.Slice` call with `slices.Sort`. The set of analyzers depends on the
    Go toolchain version, so upgrading Go may produce new findings.

    Each hunk of `go fix -diff` output is emitted as a separate finding, with a
    replacement so that `shac fix` can apply it. Applying all of them is
    equivalent to running `go fix ./...` once. A finding's message is the message of
    each `go fix` diagnostic within the hunk, if any; some hunks, such as one
    that adds an import, support a fix elsewhere in the file.

    See https://pkg.go.dev/cmd/fix for more details.

    Args:
      ctx: A ctx instance.
    """
    env = go_env(ctx)
    json_proc = ctx.os.exec(["go", "fix", "-json", "./..."], env = env)

    # The changes come from `go fix -diff` rather than from applying the -json
    # output's suggested fixes, because `go fix` does more than apply the
    # edits: it merges and drops conflicting fixes, removes imports that the
    # fixes made unused, formats the result, and skips generated files.
    # `go fix -diff` exits 1 if there are any diffs.
    diff_proc = ctx.os.exec(["go", "fix", "-diff", "./..."], env = env, ok_retcodes = (0, 1))

    changes_by_file = _parse_diff(ctx, diff_proc.wait().stdout)

    # Diagnostics outside of all hunks aren't reported, so the check fails if
    # and only if `go fix` would change something. These include diagnostics
    # in generated files, which `go fix` doesn't change, and diagnostics whose
    # fixes `go fix` dropped because they conflicted with other fixes, which
    # will show up in the diff after the other fixes are applied.
    messages = {}
    for finding in _parse_analyzer_json(json_proc.wait().stdout, "go fix"):
        filepath, line, _ = _parse_posn(ctx, finding["posn"], "go fix")
        for i, change in enumerate(changes_by_file.get(filepath, [])):
            if change["line"] <= line and line <= change["end_line"]:
                hunk_messages = messages.setdefault((filepath, i), [])
                if finding["message"] not in hunk_messages:
                    hunk_messages.append(finding["message"])
                break

    for filepath, changes in changes_by_file.items():
        for i, change in enumerate(changes):
            ctx.emit.finding(
                level = "error",
                filepath = filepath,
                line = change["line"],
                end_line = change["end_line"],
                message = "\n".join(messages.get((filepath, i), [])) or
                          "`go fix` suggests this change as part of a fix elsewhere in this file.",
                replacements = [change["replacement"]],
            )

gofix = shac.check(_gofix)

def _relpath(ctx, path):
    """Converts an absolute path from Go tooling to a root-relative path.

    Returns None if the path isn't within the root.
    """

    # Use string operations rather than a regex containing the root, since the
    # root may contain regex metacharacters, e.g. backslashes on Windows.
    path = path.replace("\\", "/")
    root = ctx.scm.root.replace("\\", "/") + "/"
    if not path.startswith(root):
        return None
    return path[len(root):]

def _parse_posn(ctx, posn, tool, allow_invalid = False):
    """Parses a "file:line[:col]" position into (relpath, line, col).

    If the position is invalid, returns None if allow_invalid is set, and fails
    otherwise.
    """

    # The non-greedy match takes the first ":<line>" suffix that leaves only an
    # optional ":<col>", which handles colons in Windows drive letters.
    match = ctx.re.match(r"^(.+?):(\d+)(?::(\d+))?$", posn)
    filepath = _relpath(ctx, match.groups[1]) if match else None
    if not filepath:
        if allow_invalid:
            return None
        fail("unrecognized position %r in `%s` output" % (posn, tool))
    col = int(match.groups[3]) if match.groups[3] else None
    return filepath, int(match.groups[2]), col

def _parse_analyzer_json(stdout, tool):
    """Returns the diagnostics from `go vet -json` or `go fix -json` output."""

    # The output is a stream of JSON objects, one per package, which exits 0
    # even if there are diagnostics. JSON strings can't contain literal
    # newlines and the objects' contents are indented, so "}\n{" only occurs
    # between objects.
    diagnostics = []
    for pkg_output in json.decode("[%s]" % stdout.strip().replace("}\n{", "},{")):
        for pkg_findings in pkg_output.values():
            for analyzer, findings in pkg_findings.items():
                # Analyzers that fail on a package report {"error": "..."}
                # instead of a list of findings.
                if type(findings) == "dict":
                    fail("`%s` analyzer %s failed: %s" % (tool, analyzer, findings.get("error")))
                diagnostics.extend(findings)
    return diagnostics

def _emit_analyzer_finding(ctx, finding, tool):
    filepath, line, col = _parse_posn(ctx, finding["posn"], tool)
    end_kwargs = {}

    # `end` may be missing or "-".
    end = _parse_posn(ctx, finding.get("end", ""), tool, allow_invalid = True)

    # Analyzers that don't set an end position report end == posn, which isn't
    # a useful span to highlight.
    if end and col and end[2] and end[0] == filepath and (end[1], end[2]) > (line, col):
        end_kwargs = {"end_line": end[1], "end_col": end[2]}

    ctx.emit.finding(
        level = "error",
        filepath = filepath,
        line = line,
        col = col,
        message = finding["message"],
        **end_kwargs
    )

def _parse_diff(ctx, diff):
    """Parses `go fix -diff` output into one change per diff hunk.

    Returns:
      A dict mapping each modified file's path to a list of changes, each a dict
      with the hunk's line range in the original file (`line` and `end_line`,
      1-based and inclusive) and the `replacement` for those lines.
    """
    changes_by_file = {}
    changes = None
    old_left, new_left = 0, 0
    for line in diff.splitlines(True):
        if old_left or new_left:
            # Hunk bodies are delimited by the header's line counts, since a
            # removed line could look like a "---" file header.
            if line.startswith("\\"):
                # "\ No newline at end of file". `go fix` output is always
                # formatted, so this only applies to removed lines.
                continue
            if not line.startswith("+"):
                old_left -= 1
            if not line.startswith("-"):
                changes[-1]["replacement"] += line[1:]
                new_left -= 1
        elif line.startswith("--- ") and line.endswith(" (old)\n"):
            filepath = _relpath(ctx, line[len("--- "):-len(" (old)\n")])
            if not filepath:
                fail("unrecognized file in `go fix -diff` output: %r" % line)
            changes = changes_by_file.setdefault(filepath, [])
        elif not line.startswith("+++ "):
            # A missing count means 1. `go fix` always includes context lines,
            # so there's no ambiguity with an empty range.
            hunk = ctx.re.match(r"^@@ -(\d+)(?:,(\d+))? \+\d+(?:,(\d+))? @@", line)
            if not hunk or changes == None:
                fail("unrecognized line in `go fix -diff` output: %r" % line)
            start = int(hunk.groups[1])
            old_left = int(hunk.groups[2] or 1)
            new_left = int(hunk.groups[3] or 1)
            changes.append({
                "line": start,
                "end_line": start + old_left - 1,
                "replacement": "",
            })
    if old_left or new_left:
        fail("truncated `go fix -diff` output")
    return changes_by_file
