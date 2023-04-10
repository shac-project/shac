# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  ctx.emit.annotation(
      level="warning",
      message="please fix",
      file="file.txt",
      span=((1,1), (10,1)),
      replacements=("nothing", "broken code"))
  ctx.emit.annotation(level="notice", span=((100,2),), message="great code")

shac.register_check(cb)
