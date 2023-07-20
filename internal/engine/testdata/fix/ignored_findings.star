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
      level="warning",  # Warnings do not trigger fixes.
      filepath="file.txt",
      message="Just a warning",
      replacements=["IGNORED"])
  ctx.emit.finding(
      level="error",
      filepath="file.txt",
      message="Error, but multiple options",
      line=1,
      # Multiple replacements do not trigger fixes.
      replacements=["IGNORED", "OTHER_IGNORED"])

shac.register_check(cb)