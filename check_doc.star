# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

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
    fail("stdlib.star needs to be updated. Want:\n%s\nGot:\n%s" % (want, got))
  want = _struct_signature(shac)
  got = _struct_signature(doc_shac)
  if want != got:
    fail("stdlib.star needs to be updated. Want:\n%s\nGot:\n%s" % (want, got))


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
