# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  # No match.
  print(shac.re.allmatches("something", "else"))
  # Both matches.
  print(shac.re.allmatches("TODO\\([^)]+\\)", "foo TODO(foo) TODO(bar)"))
  # Two capture groups.
  print(shac.re.allmatches("a(.)(.)", "ancient"))

register_check(cb)
