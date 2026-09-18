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

# Models a misbehaving check whose whole-file replacement oscillates: each pass
# it toggles the case of the first line, alongside an overlapping single-line
# replacement that can therefore never be applied.
#
# The file alternates between two states, so the state after the third pass is
# the same as after the first. Fix() must detect the repeated state and fail
# then, rather than running out the pass bound.

def cb(ctx):
    lines = str(ctx.io.read_file("file.txt")).splitlines()
    lines[0] = "THESE ARE" if lines[0] == "These are" else "These are"
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Toggle the case of the first line",
        replacements = ["\n".join(lines) + "\n"],
    )
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Always overlaps with the whole-file replacement",
        line = 1,
        replacements = ["NEVER APPLIED\n"],
    )

shac.register_check(cb)
