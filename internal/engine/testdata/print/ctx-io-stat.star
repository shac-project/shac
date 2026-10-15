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

def cb(ctx):
    print("file:", ctx.io.stat("content.json"))
    print("directory:", ctx.io.stat("dir"))
    print("missing:", ctx.io.stat("does-not-exist.txt"))
    print("parent is a file:", ctx.io.stat("content.json/child"))
    print("absolute file is_file:", ctx.io.stat(ctx.scm.root + "/content.json").is_file)
    print("absolute missing:", ctx.io.stat(ctx.scm.root + "/does-not-exist.txt"))

shac.register_check(cb)
