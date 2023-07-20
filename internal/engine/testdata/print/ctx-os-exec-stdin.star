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
  cmd = ["go", "run", "ctx-os-exec-stdin.go"]
  test_inputs = [
    None,
    "hello\nfrom\nstdin\nstring",
    b"hello\nfrom\nstdin\nbytes",
  ]

  procs = []
  for stdin in test_inputs:
    procs.append(ctx.os.exec(cmd, env = _go_env(ctx), stdin = stdin))

  for i, proc in enumerate(procs):
    res = proc.wait()
    stdin = test_inputs[i]
    print("stdout given %s for stdin:\n%s" % (type(stdin), res.stdout))

def _go_env(ctx):
  return {
    "CGO_ENABLED": "0",
    "GOCACHE": ctx.io.tempdir() + "/gocache",
    "GOPACKAGESDRIVER": "off",
  }

shac.register_check(cb)
