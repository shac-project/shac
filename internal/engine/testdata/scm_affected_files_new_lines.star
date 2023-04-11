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
  out = ""
  # Only print the first file.
  # Note: This is not super efficient as for large delta, the whole list would
  # be serialized to only then take the first element of the list.
  path, meta = ctx.scm.affected_files().items()[0]
  out += path + "\n"
  # Only print the first line.
  new_lines = meta.new_lines()
  if new_lines:
    num, line = new_lines[0]
    out += str(num) + ": " + line
    print(out)
  else:
    print("no new lines")

shac.register_check(cb)
