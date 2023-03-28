# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  """Prints the added TODOs."""
  out = ""
  for name, meta in shac.scm.affected_files().items():
    for num, line in meta.new_lines():
      m = shac.re.match("TODO\\(([^)]+)\\).*", line)
      if m:
        # TODO(maruel): Validate m.groups[1] once we can emit results (errors).
        out += "\n" + name + "(" + str(num) + "): " + m.groups[0]
  if out:
    print(out)

register_check(cb)
