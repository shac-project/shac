# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# This file contains pseudo-code that represents shac's runtime standard
# library solely for documentation purpose.

"""shac runtime standard library

The starlark language specification is documented at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md. It is a python
derivative.

Note: The standard library is implemented in native Go.
"""


def _exec(cmd, cwd = None):
  """Runs a command as a subprocess.

  Args:
    cmd: Subprocess command line.
    cwd: Relative path to cwd for the subprocess.

  Returns:
    An integer corresponding to the subprocess exit code.
  """
  pass


def _io_read_file(path):
  """Returns the content of a file.

  Args:
    path: path of the file to read. The file must be within the workspace. The
      path must be relative and in POSIX format, using / separator.

  Returns:
    Content of the file as bytes.
  """
  pass


def _re_allmatches(pattern, str):
  """Returns all the matches of the regexp pattern onto content.

  Args:
    pattern: regexp to run. It must use the syntax as described at
      https://golang.org/s/re2syntax.
    str: string to run the regexp on.

  Returns:
    list(struct(offset=bytes_offset, groups=list(matches)))
  """
  pass


def _re_match(pattern, str):
  """Returns the first match of the regexp pattern onto content.

  Args:
    pattern: regexp to run. It must use the syntax as described at
      https://golang.org/s/re2syntax.
    str: string to run the regexp on.

  Returns:
    struct(offset=bytes_offset, groups=list(matches))
  """
  pass


def _scm_affected_files(glob = None):
  """Returns affected files as determined by the SCM.

  If shac detected that the tree is managed by a source control management
  system, e.g. git, it will detect the upstream branch and return only the files
  currently modified.

  If the current directory is not controlled by a SCM, the result is equivalent
  to shac.scm.all_files().

  If shac is run with the --all options, all files are considered "added" to do
  a full run on all files.

  Args:
    glob: TODO: Will later accept a glob.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_line().
  """
  pass


def _scm_all_files(glob = None):
  """Returns all files found in the current workspace.

  It considers all files "added".

  Args:
    glob: TODO: Will later accept a glob.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_line().
  """
  pass


def register_check(cb):
  """Registers a shac check.

  Args:
    cb: Starlark function that is called back to implement the check. Passed a
      single argument shac(...).
  """
  pass


# shac is the object passed to register_check(...) callback.
shac = struct(
  exec = _exec,
  # shac.io is the object that exposes the API to interact with the file system.
  io = struct(
    read_file = _io_read_file,
  ),
  # shac.re is the object that exposes the API to run regular expressions on
  # starlark strings.
  re = struct(
    allmatches = _re_allmatches,
    match = _re_match,
  ),
  # shac.scm is the object exposes the API to query the source control
  # management (e.g. git).
  scm = struct(
    affected_files = _scm_affected_files,
    all_files = _scm_all_files,
  ),
)
