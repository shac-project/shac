# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(ctx):
  fail("an", "unexpected", "failure", None, sep="  ", unknown="invalid")

shac.register_check(cb)
