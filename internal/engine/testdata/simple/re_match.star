# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  # No match.
  print(shac.re.match("something", "else"))
  # Only first match.
  print(shac.re.match("TODO\\([^)]+\\)", "foo TODO(foo) TODO(bar)"))
  # Two capture groups.
  print(shac.re.match("a(.)(.)", "ancient"))

register_check(cb)
