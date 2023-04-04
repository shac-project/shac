# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

"""Checks for shac itself

This file will evolve as new shac functionality is being added.
"""

load("go.star", "gosec")
load("licenses.star", "check_license_headers")


def new_todos(ctx):
  """Prints the added TODOs.

  Args:
    ctx: A ctx instance.
  """
  out = ""
  for path, meta in ctx.scm.affected_files().items():
    for num, line in meta.new_lines():
      m = ctx.re.match("TODO\\(([^)]+)\\).*", line)
      if m:
        # TODO(maruel): Validate m.groups[1] once we can emit results (errors).
        out += "\n" + path + "(" + str(num) + "): " + m.groups[0]
  if out:
    print(out)
  if ctx.exec(["echo", "hello world"]) != 0:
    fail("failed to run echo")


register_check(new_todos)
register_check(gosec)
register_check(check_license_headers)
