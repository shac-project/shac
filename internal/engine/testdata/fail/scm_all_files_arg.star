# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  shac.scm.all_files("unexpected")

register_check(cb)
