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
  for path, meta in ctx.scm.affected_files().items():
    for num, line in meta.new_lines():
      m = ctx.re.match("TODO\\(([^)]+)\\).*", line)
      if m:
        # TODO(maruel): Validate m.groups[1].
        ctx.emit.annotation(
            level="notice",
            message=m.groups[0],
            file=path,
            span=((num, 1),),
        )


shac.register_check(check_docs)
shac.register_check(check_license_headers)
shac.register_check(lambda ctx: gosec(ctx))
shac.register_check(new_todos)
shac.register_check(lambda ctx: staticcheck(ctx))
