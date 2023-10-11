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
        cmd = ["cmd.exe", "/c", "hello_world.bat"]
    else:
        cmd = ["./hello_world.sh"]

    # Launch more parallel subprocesses than shac will actually allow to run in
    # parallel, i.e. more than any realistic machine will have cores (but not
    # too many, or the test will be very slow).
    num_procs = 1000
    procs = [ctx.os.exec(cmd) for _ in range(num_procs)]

    # It should be possible to wait on the subprocesses in the reverse of the
    # order in which they were started without causing a deadlock; the lock
    # should be released asynchronously, not by calling wait().
    for proc in reversed(procs):
        res = proc.wait()
        print(res.stdout.strip())

shac.register_check(cb)
