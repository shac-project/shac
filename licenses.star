# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

_EXPECTED_HEADER_RE = r"""
(#|//) Copyright \d{4} The Shac Authors\. All rights reserved\.
(#|//) Use of this source code is governed by a BSD-style
(#|//) license that can be found in the LICENSE file\.
""".strip()

_SKIP_FILE_REGEXES = [
  # All-caps files in the root directory are likely special informational files
  # that don't need licenses.
  r"[A-Z]+",
  # Markdown files and templates.
  r".*\.mdt?",
  # go.sum files can't contain comments.
  r"go\.sum",
  # JSON files can't contain comments.
  r".*\.json",
  # gitignore files need not contain a license header.
  r"(.*/)?\.gitignore"
]

def check_license_headers(ctx):
  """Checks that all files have valid license headers.

  Args:
    ctx: A ctx instance.
  """
  for path in ctx.scm.affected_files():
    if any([ctx.re.match(r"^%s$" % regex, path) for regex in _SKIP_FILE_REGEXES]):
      continue
    lines = str(ctx.io.read_file(path, 4096)).splitlines()
    # Only files with shebangs are allowed to not have a license header on the
    # first line.
    if lines[0].startswith("#!"):
      lines = lines[1:]
    if not ctx.re.match(_EXPECTED_HEADER_RE, "\n".join(lines[:3])):
      fail("%s does not start with expected license header" % path)
