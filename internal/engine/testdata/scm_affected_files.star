# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  out = "\n"
  for path, meta in ctx.scm.affected_files().items():
    out += path + ": " + meta.action + "\n"
  print(out)

shac.register_check(cb)
