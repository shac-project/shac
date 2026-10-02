# Copyright 2026 The Shac Authors
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

load("buildifier.star", "buildifier")

_GO_INSTALL_MOCK = testing.exec_mock(
    cmd = ["go", "install", "github.com/bazelbuild/buildtools/buildifier@latest"],
)

def test_buildifier_no_files():
    res = testing.run(
        buildifier,
        files = {"README.md": "# hello\n"},
    )
    asserts.eq(res.findings, ())

def test_buildifier_ignored_files():
    res = testing.run(
        buildifier,
        files = {
            "internal/engine/testdata/fail_or_throw/syntax_error.star": "x = 1\n",
        },
    )
    asserts.eq(res.findings, ())

def test_buildifier_pass():
    res = testing.run(
        buildifier,
        files = {"shac.star": "x = 1\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/buildifier", "-lint=off", "-mode=check", "shac.star"],
                retcode = 0,
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_buildifier_fail():
    def _format_tempfile(cmd):
        testing.write_file(cmd[2], "x = 1\n")

    res = testing.run(
        buildifier,
        files = {"shac.star": "x=1\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/buildifier", "-lint=off", "-mode=check", "shac.star"],
                retcode = 4,
                stderr = "shac.star # reformat\n",
            ),
            testing.exec_mock(
                cmd = [".tools/gobin/buildifier", "-lint=off", testing.any_args],
                handler = _format_tempfile,
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "shac.star",
                level = "error",
                message = "File not formatted. Run `shac fmt` to fix.",
                replacements = ("x = 1\n",),
            ),
        ),
    )
    asserts.eq(res.files["shac.star"], "x = 1\n")
