# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  ctx.emit.annotation(
      level="error",
      message="bad code",
      filepath="file.txt",
      span=((1,1), (10,1)),
      replacements=("nothing", "broken code"))

shac.register_check(cb)