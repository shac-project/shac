# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

"""Checks for shac itself

This file will evolve as new shac functionality is being added.
"""

load("//check_doc.star", "check_docs")
load("//go.star", "gosec", "staticcheck")
load("//licenses.star", "check_license_headers")


def _is_todo_valid(ctx, s):
  """Returns True if the x part of "TODO(x): y" is valid."""
  # For some project, it could be a bug number or an URL to a bug.
  return bool(ctx.re.match("^[a-z]+$", s))


def new_todos(ctx):
  """Prints the added TODOs.

  Args:
    ctx: A ctx instance.
  """
  for path, meta in ctx.scm.affected_files().items():
    for num, line in meta.new_lines():
      m = ctx.re.match("TODO\\(([^)]+)\\).*", line)
      if not m:
        continue
      # TODO(maruel): Have ctx.re.match() return the offset since it's
      # inefficient to calculate back.
      span = ((num, line.index(m.groups[0])+1), (num, len(line)))
      if _is_todo_valid(ctx, m.groups[1]):
        ctx.emit.annotation(level="notice", message=m.groups[0], file=path, span=span)
      else:
        ctx.emit.annotation(
            level="error",
            message="Use a valid username in your TODO, %r is not valid" % m.groups[1],
            file=path,
            span=span,
        )


shac.register_check(check_docs)
shac.register_check(check_license_headers)
shac.register_check(lambda ctx: gosec(ctx))
shac.register_check(new_todos)
shac.register_check(lambda ctx: staticcheck(ctx))
