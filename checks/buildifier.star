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

load("common.star", "go_install")

_IGNORE_FILES = set([
    "internal/engine/testdata/fail_or_throw/load-no_symbol.star",
    "internal/engine/testdata/fail_or_throw/syntax_error.star",
])

def _buildifier(ctx):
    """Checks Starlark/Bazel file formatting using buildifier."""
    starlark_files = [
        f
        for f in ctx.scm.affected_files()
        if f.endswith(".star") and
           f not in _IGNORE_FILES
    ]
    if not starlark_files:
        return

    exe = go_install(ctx, "github.com/bazelbuild/buildtools/buildifier", "latest")
    base_cmd = [exe, "-lint=off"]

    res = ctx.os.exec(
        base_cmd + ["-mode=check"] + starlark_files,
        ok_retcodes = (0, 4),
    ).wait()
    if res.retcode == 0:
        return

    lines = res.stderr.splitlines()
    suffix = " # reformat"

    tempfiles = {}
    for line in lines:
        if not line.endswith(suffix):
            continue
        filepath = line[:-len(suffix)]

        # Buildifier doesn't have a dry-run mode that prints the formatted file
        # to stdout, so copy each file to a temporary file and format the
        # temporary file in-place to obtain the formatted result.
        tempfiles[filepath] = ctx.io.tempfile(
            ctx.io.read_file(filepath),
            name = filepath,
        )

    ctx.os.exec(base_cmd + tempfiles.values()).wait()

    for filepath, temp in tempfiles.items():
        formatted = ctx.io.read_file(temp)
        ctx.emit.finding(
            # TODO(olivernewman): Switch to "error" after fixing formatting.
            level = "error",
            filepath = filepath,
            replacements = [str(formatted)],
        )

buildifier = shac.check(_buildifier, formatter = True)
