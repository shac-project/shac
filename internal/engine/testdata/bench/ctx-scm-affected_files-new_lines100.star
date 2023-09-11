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

    # Only print the first file, but still load the data for the other files.
    for i, (path, metadata) in enumerate(ctx.scm.affected_files().items()):
        new_lines = metadata.new_lines()
        if not i:
            # Only print the first line.
            num, line = new_lines[0]
            print(path + "\n" + str(num) + ": " + line)

def reg():
    for i in range(100):
        shac.register_check(shac.check(cb, name = "cb%d" % i))

reg()
