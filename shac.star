# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  for name, meta in shac.scm.affected_files().items():
    print(name)

register_check(cb)
