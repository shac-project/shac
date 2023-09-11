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

def cb(ctx):
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Replace the whole file",
        replacements = ["this text is a replacement\nfor the entire file\n"],
    )

    # Other findings should be ignored because they overlap with the first one.
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Change this line",
        line = 1,
        replacements = ["new line 1 content"],
    )
    ctx.emit.finding(
        level = "error",
        filepath = "file.txt",
        message = "Change this line",
        line = 2,
        replacements = ["new line 2 content"],
    )

shac.register_check(cb)
