# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  # No match.
  print(ctx.re.match("something", "else"))
  # Only first match.
  print(ctx.re.match("TODO\\([^)]+\\)", "foo TODO(foo) TODO(bar)"))
  # Two capture groups.
  print(ctx.re.match("a(.)(.)", "ancient"))
  # Optional group with no match.
  print(ctx.re.match(r"a(b)?", "a"))

register_check(cb)
