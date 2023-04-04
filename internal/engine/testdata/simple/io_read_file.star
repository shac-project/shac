# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  d = json.decode(str(ctx.io.read_file("content.json")))
  print(d)

register_check(cb)
