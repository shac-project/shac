# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

"""Checks for shac itself

This file will evolve as new shac functionality is being added.
"""

load("//check_doc.star", "check_docs")
load("//go.star", "gosec", "staticcheck")
load("//licenses.star", "check_license_headers")


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


shac.register_check(check_docs)
shac.register_check(check_license_headers)
shac.register_check(gosec)
shac.register_check(new_todos)
shac.register_check(staticcheck)
