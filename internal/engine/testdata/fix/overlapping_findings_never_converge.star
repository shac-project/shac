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

# Models a misbehaving check whose fixes never converge: every pass it emits a
# whole-file replacement that appends a line, plus an overlapping single-line
# replacement that can therefore never be applied.
#
# Fix() must give up after a bounded number of passes rather than looping
# forever. The number of "PASS" lines appended to the file is the number of
# passes that were run.

def cb(ctx):
    content = str(ctx.io.read_file("file.txt"))
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Append a line",
        replacements = [content + "PASS\n"],
    )
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Always overlaps with the whole-file replacement",
        line = 1,
        replacements = ["NEVER APPLIED\n"],
    )

shac.register_check(cb)
