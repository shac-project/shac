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
    env = go_env(ctx)
    cache = _go_cache(ctx)

    # Save executables in a common directory.
    env["GOBIN"] = cache + "/gobin"

    tool_name = pkg.split("/")[-1]

    # Setting GOPATH is necessary on Windows when USERPROFILE is not set.
    # Use one GOPATH per tool. Since both GOBIN and GOMODCACHE are redirected,
    # in practice only sumdb is going to be unique
    env["GOPATH"] = cache + "/" + tool_name

    ctx.os.exec(
        ["go", "install", "%s@%s" % (pkg, version)],
        allow_network = True,
        env = env,
    ).wait()

    tool_exec = tool_name
    if ctx.platform.os == "windows":
        tool_exec += ".exe"
    return "%s/%s" % (env["GOBIN"], tool_exec)

def go_env(ctx):
    """Returns environment variables to use when running Go tooling."""
    cache = _go_cache(ctx)
    return {
        # Disable cgo as it's not necessary and not all development platforms have
        # the necessary headers.
        "CGO_ENABLED": "0",
        "GOFLAGS": " ".join([
            # Disable embedding VCS information because it causes ineffassign builds
            # to fail on some machines.
            "-buildvcs=false",
        ]),
        # Share the Go module cache to reduce downloads.
        "GOMODCACHE": cache + "/mod",
        # TODO(olivernewman): The default gopackagesdriver is broken within an
        # nsjail.
        "GOPACKAGESDRIVER": "off",
    }

def _go_cache(ctx):
    """Returns the shared Go cache."""

    # TODO(olivernewman): Implement proper cache directories for shac instead of
    # creating a `.tools` directory, which requires making the root directory
    # writable.
    return ctx.scm.root + "/.tools"
