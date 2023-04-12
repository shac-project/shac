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

"""Checks for shac itself

This file will evolve as new shac functionality is being added.
"""

load("//check_doc.star", "check_docs")
load("//go.star", "gosec", "ineffassign", "shadow", "staticcheck")
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
      if _is_todo_valid(ctx, m.groups[1]):
        level = "notice"
        message = m.groups[0]
      else:
        level = "error"
        message = "Use a valid username in your TODO, %r is not valid" % m.groups[1]
      ctx.emit.annotation(
          level=level,
          message=message,
          filepath=path,
          line=num,
          col=line.index(m.groups[0])+1,
          end_line=num,
          end_col=len(line),
      )


shac.register_check(check_docs)
shac.register_check(check_license_headers)
shac.register_check(lambda ctx: gosec(ctx), name="gosec")
shac.register_check(lambda ctx: ineffassign(ctx), name="ineffassign")
shac.register_check(new_todos)
shac.register_check(lambda ctx: shadow(ctx), name="shadow")
shac.register_check(lambda ctx: staticcheck(ctx), name="staticcheck")
