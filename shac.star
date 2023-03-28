# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  for name, meta in shac.scm.affected_files().items():
    todos = shac.re.allmatches("TODO\\(([^)]+)\\).*", str(shac.io.read_file(name)))
    if todos:
      out = name + "\n"
      for m in todos:
        # TODO(maruel): Validate m.groups[1] once we can emit results (errors).
        out += str(m.offset) + ": " + m.groups[0] + "\n"
      print(out)

register_check(cb)
