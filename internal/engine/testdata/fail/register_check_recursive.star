# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb2(ctx):
  pass

def cb1(ctx):
  shac.register_check(cb2)

shac.register_check(cb1)
