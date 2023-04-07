# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  out = ""
  # Only print the first file.
  # Note: This is not super efficient as for large delta, the whole list would
  # be serialized to only then take the first element of the list.
  path, meta = ctx.scm.affected_files().items()[0]
  out += path + "\n"
  # Only print the first line.
  new_lines = meta.new_lines()
  if new_lines:
    num, line = new_lines[0]
    out += str(num) + ": " + line
    print(out)
  else:
    print("no new lines")

shac.register_check(cb)
