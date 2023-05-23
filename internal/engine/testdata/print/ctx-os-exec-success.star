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
    cmd = ["cmd.exe", "/c", "stdio.bat"]
  else:
    cmd = ["./stdio.sh"]
  res = ctx.os.exec(cmd)
  print("retcode: %d" % res.retcode)
  print("stdout: %s" % res.stdout.strip())
  print("stderr: %s" % res.stderr.strip())

shac.register_check(cb)
