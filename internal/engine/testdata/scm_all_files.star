# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

def cb(shac):
  out = "\n"
  for path, meta in shac.scm.all_files().items():
    out += path + ": " + meta.action + "\n"
  print(out)

register_check(cb)
