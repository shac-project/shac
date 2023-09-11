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
        cmd = ["cmd.exe", "/c", "stdio.bat"]
    else:
        cmd = ["./stdio.sh"]
    res = ctx.os.exec(cmd).wait()

    # Emit everything as a single statement. Since each check run in parallel,
    # each of the print statement can be interleaved by other checks running
    # concurrently.
    print("retcode: %d\nstdout: %s\nstderr: %s" % (res.retcode, res.stdout.strip(), res.stderr.strip()))

def reg():
    for i in range(100):
        shac.register_check(shac.check(cb, name = "cb%d" % i))

reg()
