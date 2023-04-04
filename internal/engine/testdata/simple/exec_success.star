# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  print("retcode: %d" % shac.exec(["echo", "hello world"]))

register_check(cb)
