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
  first = ctx.io.tempfile("first\nfile\ncontents\n")
  second = ctx.io.tempfile(b"contents\nof\nsecond\nfile\n", name="dir/second.txt")
  print(ctx.io.read_file(first))
  print(ctx.io.read_file(second))

shac.register_check(cb)
