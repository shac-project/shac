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

# Same whole-file formatter and line-range fixer as
# overlapping_findings_converge.star, except that the line-range fixer's
# replacement records which .txt files were affected when it ran.
#
# The line-range finding is skipped in the first pass and applied in the
# second, so the recorded files are the ones the second pass saw. When files
# were passed explicitly, the second pass must be narrowed to the files with
# skipped findings (just file.txt); otherwise all files remain affected.

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
    affected = " ".join(sorted(ctx.scm.affected_files(glob = "*.txt").keys()))
    for i, line in enumerate(_lines(ctx)):
        if line == "of the file":
            ctx.emit.finding(
                level = "error",
                filepath = "file.txt",
                message = "Update this line",
                line = i + 1,
                replacements = ["UPDATED %s\n" % affected],
            )

shac.register_check(whole_file)
shac.register_check(one_line)
