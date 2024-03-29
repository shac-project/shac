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
    res = ctx.os.exec(
        [
            "go",
            "run",
            "ctx-os-exec-passthrough_env.go",
            ctx.vars.get("VAR_PREFIX"),
        ],
        env = _go_env(ctx),
    ).wait()
    print(res.stdout.rstrip())

def _go_env(ctx):
    return {
        "CGO_ENABLED": "0",
        "GOPACKAGESDRIVER": "off",
        # Explicitly set GOROOT to prevent warnings about GOROOT and GOPATH being
        # equal when they're both empty.
        "GOROOT": ctx.os.exec(["go", "env", "GOROOT"]).wait().stdout.strip(),
    }

shac.register_check(cb)
