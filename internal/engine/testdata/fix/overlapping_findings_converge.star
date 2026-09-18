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

# Models a whole-file formatter (e.g. black, gofmt, rustfmt) running alongside a
# line-range fixer (e.g. keep-sorted) on the same file. Both checks only emit a
# finding when the file actually needs fixing, so they converge.
#
# In the first pass only the whole-file replacement is applied, since the
# line-range replacement overlaps with it. Fix() must then re-run the checks
# and apply the line-range replacement in a second pass.

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

shac.register_check(whole_file)
shac.register_check(one_line)
