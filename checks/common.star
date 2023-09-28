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

def go_install(ctx, pkg, version):
    """Runs `go install`."""

    env = go_env()

    # TODO(olivernewman): Implement proper cache directories for shac instead of
    # creating a `.tools` directory, which requires making the root directory
    # writable.
    env["GOBIN"] = ctx.scm.root + "/.tools/gobin"

    # TODO(olivernewman): Stop using a separate GOPATH for each tool, and instead
    # install the tools sequentially. Multiple concurrent `go install` runs on the
    # same GOPATH results in race conditions.
    ctx.os.exec(
        ["go", "install", "%s@%s" % (pkg, version)],
        allow_network = True,
        env = env,
    ).wait()

    tool_name = pkg.split("/")[-1]
    return "%s/%s" % (env["GOBIN"], tool_name)

def go_env():
    return {
        # Disable cgo as it's not necessary and not all development platforms have
        # the necessary headers.
        "CGO_ENABLED": "0",
        "GOFLAGS": " ".join([
            # Disable embedding VCS information because it causes ineffassign builds
            # to fail on some machines.
            "-buildvcs=false",
        ]),
        # TODO(olivernewman): The default gopackagesdriver is broken within an
        # nsjail.
        "GOPACKAGESDRIVER": "off",
    }
