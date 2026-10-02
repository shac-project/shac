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

load("licenses.star", "check_license_headers")

_VALID_HASH_HEADER = """# Copyright 2026 The Shac Authors
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
"""

_VALID_SLASH_HEADER = """// Copyright 2026 The Shac Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
"""

def test_licenses_pass():
    res = testing.run(
        check_license_headers,
        files = {
            "foo.star": _VALID_HASH_HEADER + "\nx = 1\n",
            "main.go": _VALID_SLASH_HEADER + "\npackage main\n",
            "script.sh": "#!/bin/sh\n" + _VALID_HASH_HEADER + "\necho hi\n",
        },
    )
    asserts.eq(res.findings, ())

def test_licenses_ignored_files():
    res = testing.run(
        check_license_headers,
        files = {
            "README.md": "no license",
            "doc/template.mdt": "no license",
            "go.sum": "no license",
            "config.json": "{}",
            ".gitignore": "/bin",
            "internal/testdata/sample.txt": "no license",
            "OWNERS": "alice@example.com",
            "binary.bin": "hello\x00world",
        },
    )
    asserts.eq(res.findings, ())

def test_licenses_fail():
    res = testing.run(
        check_license_headers,
        files = {
            "bad.go": "package main\n",
        },
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                level = "error",
                message = "bad.go does not start with expected license header",
            ),
        ),
    )
