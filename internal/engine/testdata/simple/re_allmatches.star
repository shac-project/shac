# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  # No match.
  print(ctx.re.allmatches("something", "else"))
  # Both matches.
  print(ctx.re.allmatches("TODO\\([^)]+\\)", "foo TODO(foo) TODO(bar)"))
  # Two capture groups.
  print(ctx.re.allmatches("a(.)(.)", "ancient"))

register_check(cb)
