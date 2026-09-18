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

# Three checks whose skipped findings shrink from one pass to the next, so
# that a re-run pass writes exactly the same bytes as the previous one and yet
# the fixes still converge.
#
# Pass 1: on file.txt, noop's whole-file no-op wins over one_line; on
# other.txt, other_noop's whole-file no-op wins over noop's line-1 no-op. Both
# noop and one_line are re-run. Pass 2: noop still wins on file.txt and now
# applies its no-op on other.txt, so the files are written unchanged again, but
# only one_line is left to re-run. Pass 3: one_line is applied.
#
# Fix() must not mistake the repeated write for a cycle: the input of pass 3
# (which checks run) differs from that of pass 2, so it must run.

def _lines(ctx):
    return str(ctx.io.read_file("file.txt")).splitlines()

def noop(ctx):
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Replace the file with itself",
        replacements = [str(ctx.io.read_file("file.txt"))],
    )
    other = str(ctx.io.read_file("other.txt")).splitlines()
    ctx.emit.finding(
        level = "error",
        filepath = "other.txt",
        message = "Replace the first line with itself",
        line = 1,
        replacements = [other[0] + "\n"],
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

def other_noop(ctx):
    ctx.emit.finding(
        level = "error",
        filepath = "other.txt",
        message = "Replace the file with itself",
        replacements = [str(ctx.io.read_file("other.txt"))],
    )

shac.register_check(noop)
shac.register_check(one_line)
shac.register_check(other_noop)
