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

load("//shac.star", "check_commit_subject_length", "new_todos", "suggest_version_bump")

def test_suggest_version_bump_go_files_without_version_file():
    res = testing.run(
        suggest_version_bump,
        files = {
            "main.go": "package main\n",
            "internal/engine/version.go": testing.file(
                content = "package engine\n",
                affected = False,
            ),
        },
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "internal/engine/version.go",
                level = "notice",
                message = "Consider updating the shac version when making API changes.",
            ),
        ),
    )

def test_suggest_version_bump_go_files_with_version_file():
    res = testing.run(
        suggest_version_bump,
        files = {
            "main.go": "package main\n",
            "internal/engine/version.go": "package engine\n",
        },
    )
    asserts.eq(res.findings, ())

def test_suggest_version_bump_test_or_non_go_files():
    res = testing.run(
        suggest_version_bump,
        files = {
            "main_test.go": "package main\n",
            "README.md": "# hello\n",
        },
    )
    asserts.eq(res.findings, ())

def test_check_commit_subject_length_pass():
    res = testing.run(
        check_commit_subject_length,
        commits = [
            testing.commit(hash = "abcdef", message = "Short subject line\n\nBody text."),
        ],
    )
    asserts.eq(res.findings, ())

def test_check_commit_subject_length_fail():
    res = testing.run(
        check_commit_subject_length,
        commits = [
            testing.commit(
                hash = "abcdef",
                message = "This is a very long commit subject line that exceeds fifty characters",
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                commit_hash = "abcdef",
                level = "warning",
                line = 1,
                col = 51,
                message = "Commit message subject line should be under 50 characters",
            ),
        ),
    )

def test_new_todos():
    res = testing.run(
        new_todos,
        files = {
            "main.go": testing.file(
                content = "// not a todo\n// TO" + "DO(alice): valid\n// TO" + "DO(123): invalid\n",
                new_lines = [
                    (2, "// TO" + "DO(alice): valid"),
                    (3, "// TO" + "DO(123): invalid"),
                ],
            ),
        },
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                level = "notice",
                line = 2,
                col = 4,
                end_line = 2,
                end_col = 22,
                message = "TO" + "DO(alice): valid",
            ),
            testing.finding(
                filepath = "main.go",
                level = "error",
                line = 3,
                col = 4,
                end_line = 3,
                end_col = 22,
                message = 'Use a valid username in your TODO, "123" is not valid',
            ),
        ),
    )
