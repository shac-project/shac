# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb2():
  pass

def cb1():
  register_check(cb2)

register_check(cb1)
