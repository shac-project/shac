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

load("//doc/stdlib.star", doc_check = "check", doc_ctx = "ctx", doc_shac = "shac")

a_check = shac.check(lambda ctx: None, name = "a_check")

def check_docs(ctx):
    """Validates that structs in //doc/stdlib.star are up-to-date.

    Specifically, documented structs should (recursively) have all the same
    fields with all the same types as the real objects.

    Function signatures are not validated.

    TODO(olivernewman): Consider writing this as a Go test that can use the
    Starlark interpreter to get function signatures.

    Args:
        ctx: A ctx instance.
    """
    pairs = [
        (ctx, doc_ctx),
        (shac, doc_shac),
        (a_check, doc_check),
    ]
    for actual, documented in pairs:
        want = _struct_signature(actual)
        got = _struct_signature(documented)
        if want != got:
            ctx.emit.finding(
                level = "error",
                message = "stdlib.star needs to be updated. Want:\n%s\nGot:\n%s" % (want, got),
            )

def _struct_signature(s):
    if type(s) != type(struct()) and not type(s).startswith("shac."):
        # stdlib.star uses dummy functions instead of actual builtin functions, they
        # should be considered equivalent.
        if type(s) == "builtin_function_or_method":
            return "function"
        return type(s)
    return {
        k: _struct_signature(getattr(s, k))
        for k in dir(s)
    }
