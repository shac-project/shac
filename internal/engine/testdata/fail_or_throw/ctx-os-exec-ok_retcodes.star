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
    if ctx.platform.os == "windows":
        cmd = ["cmd.exe", "/c", "exit", "1"]
    else:
        cmd = ["false"]
    res = ctx.os.exec(cmd, ok_retcodes = [0, 1]).wait()
    if res.retcode != 1:
        fail("unexpected retcode %d" % res.retcode)

    if ctx.platform.os == "windows":
        cmd = ["cmd.exe", "/c", "exit", "0"]
    else:
        cmd = ["true"]
    ctx.os.exec(cmd, ok_retcodes = [1]).wait()
    fail("should have exited")

shac.register_check(cb)
