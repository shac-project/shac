# Copyright 2023 The Shac Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

_EXPECTED_HEADER_RE = r"""
(#|//|::) Copyright \d{4} The Shac Authors
(#|//|::)
(#|//|::) Licensed under the Apache License, Version 2\.0 \(the "License"\);
(#|//|::) you may not use this file except in compliance with the License\.
(#|//|::) You may obtain a copy of the License at
(#|//|::)
(#|//|::)     http://www\.apache\.org/licenses/LICENSE-2\.0
(#|//|::)
(#|//|::) Unless required by applicable law or agreed to in writing, software
(#|//|::) distributed under the License is distributed on an "AS IS" BASIS,
(#|//|::) WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied\.
(#|//|::) See the License for the specific language governing permissions and
(#|//|::) limitations under the License\.
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
  r"(.*/)?\.gitignore",
  # text files in testdata/ need not contain a license header.
  r"(.*/)?testdata/(.+)\.txt",
  # TODO(olivernewman): Remove after un-vendoring the slices library.
  r"internal/slices/slices.go",
]

def check_license_headers(ctx):
  """Checks that all files have valid license headers.

  Args:
    ctx: A ctx instance.
  """
  for path in ctx.scm.affected_files():
    if any([ctx.re.match(r"^%s$" % regex, path) for regex in _SKIP_FILE_REGEXES]):
      continue
    contents = str(ctx.io.read_file(path, 4096))
    # TODO(olivernewman): Add an argument to affected_files() to skip binary
    # files so they don't need to be handled hackily here.
    if "\0" in contents:
      # Assume that a file is binary if it contains a null byte in its first N
      # bytes.
      continue
    lines = contents.splitlines()
    # Only files with shebangs are allowed to not have a license header on the
    # first line.
    if lines and lines[0].startswith("#!"):
      lines = lines[1:]
    if not ctx.re.match(_EXPECTED_HEADER_RE, "\n".join(lines)):
      ctx.emit.finding(
          level="error",
          message="%s does not start with expected license header" % path)
