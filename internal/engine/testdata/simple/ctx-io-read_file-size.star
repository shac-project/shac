# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  print(str(ctx.io.read_file("content.json", size=10)))

shac.register_check(cb)
