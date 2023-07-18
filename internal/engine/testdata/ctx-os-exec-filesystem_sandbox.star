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
  res = ctx.os.exec([ctx.scm.root + "/sandbox_read.sh"], raise_on_failure = False).wait()
  print("sandbox_read.sh retcode: %d" % res.retcode)
  print(res.stderr)

  res = ctx.os.exec([ctx.scm.root + "/sandbox_write.sh"], raise_on_failure = False).wait()
  print("sandbox_write.sh retcode: %d" % res.retcode)
  print(res.stderr)

shac.register_check(cb)
