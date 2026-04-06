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
    out = "\n"

    # Test with single string glob
    for path, _ in ctx.scm.all_files(glob = "*all_files.star").items():
        out += "glob=*all_files.star: " + path + "\n"

    # Test with list of strings glob
    for path, _ in ctx.scm.all_files(glob = ["*all_files.star", "non-existent.star"]).items():
        out += "glob=[*all_files.star, non-existent.star]: " + path + "\n"

    # Test with no match
    for path, _ in ctx.scm.all_files(glob = "nothing-matches.txt").items():
        out += "glob=nothing-matches.txt: " + path + "\n"
    print(out)

shac.register_check(cb)
