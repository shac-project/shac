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

load("//doc/stdlib.star", doc_ctx = "ctx", doc_shac = "shac")

def check_docs(ctx):
    """Validates that the `ctx` and `shac` structs in //doc/stdlib.star are
    up-to-date.

    Specifically, it should (recursively) have all the same fields with all the
    same types as the `shac` global and the `ctx` object that gets passed to
    checks.

    Function signatures are not validated.

    TODO(olivernewman): Consider writing this as a Go test that can use the
    Starlark interpreter to get function signatures.

    Args:
      ctx: A ctx instance.
    """
    want = _struct_signature(ctx)
    got = _struct_signature(doc_ctx)
    if want != got:
        ctx.emit.finding(
            level = "error",
            message = "stdlib.star needs to be updated. Want:\n%s\nGot:\n%s" % (want, got),
        )
    want = _struct_signature(shac)
    got = _struct_signature(doc_shac)
    if want != got:
        ctx.emit.finding(
            level = "error",
            message = "stdlib.star needs to be updated. Want:\n%s\nGot:\n%s" % (want, got),
        )

def _struct_signature(s):
    if type(s) != type(struct()):
        # stdlib.star uses dummy functions instead of actual builtin functions, they
        # should be considered equivalent.
        if type(s) == "builtin_function_or_method":
            return "function"
        return type(s)
    return {
        k: _struct_signature(getattr(s, k))
        for k in dir(s)
    }
