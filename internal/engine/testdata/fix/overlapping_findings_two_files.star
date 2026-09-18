# Copyright 2026 The Shac Authors
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

# Overlapping findings in one file (file.txt) alongside a single
# non-overlapping finding in another file (other.txt).
#
# other.txt must be fixed in the first pass, not delayed until file.txt's
# overlapping findings force a re-run. To make that observable, the finding on
# other.txt records which pass it was emitted in, as inferred from whether
# file.txt's first-pass fix has been applied yet (all checks in a pass see the
# same on-disk contents). file.txt converges in two passes as in
# overlapping_findings_converge.star.

def _lines(ctx):
    return str(ctx.io.read_file("file.txt")).splitlines()

def whole_file(ctx):
    lines = _lines(ctx)
    if lines[0] == "THESE ARE":
        return
    lines[0] = "THESE ARE"
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Uppercase the first line",
        replacements = ["\n".join(lines) + "\n"],
    )

def one_line(ctx):
    for i, line in enumerate(_lines(ctx)):
        if line == "of the file":
            ctx.emit.finding(
                level = "error",
                filepath = "file.txt",
                message = "Update this line",
                line = i + 1,
                replacements = ["UPDATED\n"],
            )

def other_file(ctx):
    content = str(ctx.io.read_file("other.txt"))
    if "FIXED" in content:
        return
    pass_num = 2 if _lines(ctx)[0] == "THESE ARE" else 1
    ctx.emit.finding(
        level = "error",
        filepath = "other.txt",
        message = "Append a line",
        replacements = [content + "FIXED IN PASS %d\n" % pass_num],
    )

shac.register_check(whole_file)
shac.register_check(one_line)
shac.register_check(other_file)
