# Copyright 2025 The Shac Authors
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
        message = "insert some text",
        line = 2,
        # If col==end_col, the replacement should be inserted at the specified
        # column without deleting any existing characters.
        col = 4,
        end_col = 4,
        replacements = [" INSERTED"],
    )

shac.register_check(cb)
