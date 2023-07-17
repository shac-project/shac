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
  out = "\nWith deleted:\n"
  for path, meta in ctx.scm.all_files(include_deleted = True).items():
    out += path + ": " + meta.action + "\n"

  # Now try with `include_deleted = False` to make sure the output is different
  # even when the result is cached internally.
  out += "\nWithout deleted:\n"
  for path, meta in ctx.scm.all_files(include_deleted = False).items():
    out += path + ": " + meta.action + "\n"

  print(out)

shac.register_check(cb)
