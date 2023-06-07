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
  if ctx.os.name == "windows":
    cmd = ["cmd.exe", "/c", "hello_world.bat"]
  else:
    cmd = ["./hello_world.sh"]

  procs = []
  for _ in range(10):
    procs.append(ctx.os.exec(cmd))

  for proc in procs:
    res = proc.wait()
    print(res.stdout.strip())

shac.register_check(cb)
