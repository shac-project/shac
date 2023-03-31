# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# This file contains pseudo-code that represents shac's runtime standard
# library solely for documentation purpose.
#
# The starlark language specification is documented at
# https://github.com/google/starlark-go/blob/HEAD/doc/spec.md. It is a python
# derivative.
#
# The standard library is implemented in native Go.

def _affected_files(glob = None):
  """Returns affected files.

  Args:
    glob: TODO: Will later accept a glob.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_line().
  """
  pass

# shac is the object passed to register_check(...) callback.
shac = struct(
  # shac.io exposes the API to interact with the file system.
  io = struct(
    read_file = _read_file,
  ),
  # shac.re exposes the API to run regular expressions on starlark strings.
  re = struct(
    match = _match,
    allmatches = _allmatches,
  ),
  # shac.scm exposes the API to query the source control management (e.g. git).
  scm = struct(
    affected_files = _affected_files,
    all_files = _all_files,
  ),
)

def register_check(cb):
  """Registers a shac check.

  Args:
    cb: Starlark function that is called back to implement the check. Passed a
      single argument shac(...).

  Returns:
    None
  """
  pass
