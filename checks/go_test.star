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

load("go.star", "gofix", "gofmt", "gosec", "govet", "ineffassign", "no_fork_without_lock", "shadow", "staticcheck")

_GO_INSTALL_MOCK = testing.exec_mock(
    cmd = ["go", "install", testing.any_args],
)

def test_gofmt_real_pass():
    formatted = "package main\n\nfunc main() {}\n"
    res = testing.run(
        gofmt,
        files = {"main.go": formatted},
    )
    asserts.eq(res.findings, ())
    asserts.eq(res.files["main.go"], formatted)

def test_gofmt_real_fail():
    res = testing.run(
        gofmt,
        files = {"main.go": "package main\nfunc main() {   }\n"},
    )
    want_formatted = "package main\n\nfunc main() {}\n"
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                level = "error",
                message = "needs formatting",
                replacements = (want_formatted,),
            ),
        ),
    )
    asserts.eq(res.files["main.go"], want_formatted)

def test_gofmt_mocked_no_simplify():
    res = testing.run(
        gofmt.with_args(simplify = False),
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["gofmt", "-l", "main.go"],
                stdout = "",
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_gosec_pass():
    res = testing.run(
        gosec,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [
                    ".tools/gobin/gosec",
                    "-fmt=json",
                    "-quiet",
                    "-exclude=G204,G304,G703",
                    "-exclude-dir=.tools",
                    "-exclude-dir=internal/engine/testdata",
                    "./...",
                ],
                retcode = 0,
                stdout = "",
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_gosec_fail():
    gosec_output = json.encode({
        "Golang errors": {
            testing.root + "/main.go": [
                {"line": "3", "column": "7", "error": "undefined: foo"},
            ],
            testing.root + "/unaffected.go": [
                {"line": "1", "column": "1", "error": "ignored"},
            ],
        },
        "Issues": [
            {
                "file": testing.root + "/main.go",
                "line": "12-14",
                "column": "4",
                "rule_id": "G101",
                "details": "Potential hardcoded credentials",
            },
            {
                "file": testing.root + "/unaffected.go",
                "line": "5",
                "column": "1",
                "rule_id": "G101",
                "details": "Ignored unaffected file",
            },
        ],
    })
    res = testing.run(
        gosec,
        files = {
            "main.go": "package main\n",
            "unaffected.go": testing.file(content = "package main\n", affected = False),
        },
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [
                    ".tools/gobin/gosec",
                    "-fmt=json",
                    "-quiet",
                    "-exclude=G204,G304,G703",
                    "-exclude-dir=.tools",
                    "-exclude-dir=internal/engine/testdata",
                    "./...",
                ],
                retcode = 1,
                stdout = gosec_output,
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 3,
                col = 7,
                level = "error",
                message = "undefined: foo",
            ),
            testing.finding(
                filepath = "main.go",
                line = 12,
                col = 4,
                level = "error",
                message = "G101: Potential hardcoded credentials",
            ),
        ),
    )

def test_ineffassign_pass():
    res = testing.run(
        ineffassign,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/ineffassign", "./..."],
                retcode = 0,
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_ineffassign_fail():
    line = testing.root + "/main.go:10:5: ineffectual assignment to err"
    res = testing.run(
        ineffassign,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/ineffassign", "./..."],
                retcode = 3,
                stderr = line + "\n" + line + "\n",
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 10,
                col = 5,
                level = "error",
                message = "ineffectual assignment to err",
            ),
        ),
    )

def test_staticcheck_pass():
    res = testing.run(
        staticcheck,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/staticcheck", "-f=json", "./..."],
                retcode = 0,
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_staticcheck_fail():
    finding_json = json.encode({
        "severity": "warning",
        "message": "SA1000: invalid regular expression",
        "location": {
            "file": testing.root + "/main.go",
            "line": 8,
            "column": 3,
        },
        "end": {
            "line": 8,
            "column": 12,
        },
    })
    res = testing.run(
        staticcheck,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/staticcheck", "-f=json", "./..."],
                retcode = 1,
                stdout = finding_json + "\n",
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 8,
                col = 3,
                end_line = 8,
                end_col = 12,
                level = "warning",
                message = "SA1000: invalid regular expression",
            ),
        ),
    )

def test_shadow_pass():
    res = testing.run(
        shadow,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/shadow", "-test=false", "-json", "./..."],
                stdout = "{}",
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_shadow_fail():
    shadow_out = json.encode({
        "example.com/pkg": {
            "shadow": [
                {
                    "posn": testing.root + "/main.go:15:6",
                    "message": 'declaration of "err" shadows declaration at line 10',
                },
            ],
        },
    })
    res = testing.run(
        shadow,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            _GO_INSTALL_MOCK,
            testing.exec_mock(
                cmd = [".tools/gobin/shadow", "-test=false", "-json", "./..."],
                stdout = shadow_out,
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 15,
                col = 6,
                level = "error",
                message = 'declaration of "err" shadows declaration at line 10',
            ),
        ),
    )

def test_no_fork_without_lock_pass():
    out = json.encode({
        "go.fuchsia.dev/shac-project/shac/internal/execsupport": {
            "fork_check": [
                {"posn": testing.root + "/internal/execsupport/execsupport.go:10:2", "message": "ok"},
            ],
        },
    })
    res = testing.run(
        no_fork_without_lock,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["go", "run", "./internal/go_checks/fork_check", "-test=false", "-json", "./..."],
                stdout = out,
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_no_fork_without_lock_fail():
    out = json.encode({
        "go.fuchsia.dev/shac-project/shac/internal/execsupport": {
            "fork_check": [
                {"posn": testing.root + "/internal/execsupport/execsupport.go:10:2", "message": "ok"},
            ],
        },
        "go.fuchsia.dev/shac-project/shac/internal/other": {
            "fork_check": [
                {"posn": testing.root + "/main.go:22:9", "message": "do not call cmd.Run directly"},
            ],
        },
    })
    res = testing.run(
        no_fork_without_lock,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["go", "run", "./internal/go_checks/fork_check", "-test=false", "-json", "./..."],
                stdout = out,
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 22,
                col = 9,
                level = "error",
                message = "do not call cmd.Run directly",
            ),
        ),
    )

def test_no_fork_without_lock_missing_execsupport():
    asserts.fails(
        lambda: testing.run(
            no_fork_without_lock,
            files = {"main.go": "package main\n"},
            exec_mocks = [
                testing.exec_mock(
                    cmd = ["go", "run", "./internal/go_checks/fork_check", "-test=false", "-json", "./..."],
                    stdout = "{}",
                ),
            ],
        ),
        "execsupport package was not found",
    )

def test_govet_pass():
    res = testing.run(
        govet,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["go", "vet", "-json", "-copylocks", "-unusedresult", "./..."],
                stdout = "{}\n",
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_govet_fail():
    stdout = "{}\n" + json.indent(json.encode({
        "example.com/pkg": {
            "copylocks": [
                {
                    "posn": testing.root + "/main.go:30:12",
                    "end": testing.root + "/main.go:30:12",
                    "message": "call of foo copies lock value: sync.Mutex",
                },
            ],
        },
    }), indent = "\t") + "\n{}\n"
    res = testing.run(
        govet,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["go", "vet", "-json", "-copylocks", "-unusedresult", "./..."],
                stdout = stdout,
                stderr = "warning: GOPATH set to GOROOT\n",
            ),
        ],
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 30,
                col = 12,
                level = "error",
                message = "call of foo copies lock value: sync.Mutex",
            ),
        ),
    )

def test_govet_end_positions():
    def posn(s):
        return testing.root + "/main.go:" + s

    stdout = json.indent(json.encode({
        "example.com/pkg": {
            "copylocks": [
                {"posn": posn("3:5"), "end": posn("3:9"), "message": "same line"},
                {"posn": posn("3:5"), "end": posn("5:2"), "message": "multiple lines"},
                # Ends that aren't after the start, aren't valid positions, or
                # are missing are ignored.
                {"posn": posn("3:5"), "end": posn("2:1"), "message": "end before start"},
                {"posn": posn("3:5"), "end": "-", "message": "invalid end"},
                {"posn": posn("3:5"), "message": "no end"},
                # A start without a column is reported without an end.
                {"posn": posn("3"), "end": posn("3:9"), "message": "no start column"},
            ],
        },
    }), indent = "\t") + "\n"
    res = testing.run(
        govet,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["go", "vet", "-json", "-copylocks", "-unusedresult", "./..."],
                stdout = stdout,
            ),
        ],
    )
    asserts.eq(
        [(f.message, f.line, f.col, f.end_line, f.end_col) for f in res.findings],
        [
            ("same line", 3, 5, 3, 9),
            ("multiple lines", 3, 5, 5, 2),
            ("end before start", 3, 5, 0, 0),
            ("invalid end", 3, 5, 0, 0),
            ("no end", 3, 5, 0, 0),
            ("no start column", 3, 0, 0, 0),
        ],
    )

_GOFIX_JSON_CMD = ["go", "fix", "-json", "./..."]
_GOFIX_DIFF_CMD = ["go", "fix", "-diff", "./..."]
_GOFIX_SUPPORTING_MESSAGE = "`go fix` suggests this change as part of a fix elsewhere in this file."

def _gofix_mocks(json_out = "{}\n", diff_out = ""):
    return [
        testing.exec_mock(cmd = _GOFIX_JSON_CMD, stdout = json_out),
        testing.exec_mock(cmd = _GOFIX_DIFF_CMD, stdout = diff_out, retcode = 1 if diff_out else 0),
    ]

def _gofix_json(filepath, findings):
    """Returns `go fix -json` output for a single package."""
    out = {"example.com/pkg": {"any": [
        dict(f, **{k: "%s/%s:%s" % (testing.root, filepath, f[k]) for k in ("posn", "end") if k in f})
        for f in findings
    ]}}
    return "{}\n" + json.indent(json.encode(out), indent = "\t") + "\n"

def _gofix_diff_header(filepath):
    return "--- %s/%s (old)\n+++ %s/%s (new)\n" % (testing.root, filepath, testing.root, filepath)

def test_gofix_real():
    # The fix adds a "slices" import, and `go fix` also removes the "sort"
    # import that the fix makes unused.
    original = "\n".join([
        "package main",
        "",
        "import \"sort\"",
        "",
        "func main() {",
        "\ts := []int{2, 1}",
        "\tsort.Slice(s, func(i, j int) bool { return s[i] < s[j] })",
        "}",
        "",
    ])
    want_fixed = "\n".join([
        "package main",
        "",
        "import \"slices\"",
        "",
        "func main() {",
        "\ts := []int{2, 1}",
        "\tslices.Sort(s)",
        "}",
        "",
    ])
    res = testing.run(
        gofix,
        files = {
            "go.mod": "module example.com/m\n\ngo 1.27\n",
            "main.go": original,
        },
        # The test checkout includes shac's own files, so use a subdirectory
        # to keep shac's go.mod and vendor directory out of the test module.
        subdir = "gofixtest",
    )
    asserts.eq(
        [(f.filepath, f.line) for f in res.findings],
        [("main.go", 1)],
    )
    asserts.eq(res.files["main.go"], want_fixed)

def test_gofix_pass():
    res = testing.run(
        gofix,
        files = {"main.go": "package main\n"},
        exec_mocks = _gofix_mocks(json_out = "{}\n{}\n"),
    )
    asserts.eq(res.findings, ())

def test_gofix_fail():
    original = "".join(["%d\n" % i for i in range(1, 21)])
    want_fixed = "".join(["%s\n" % x for x in (
        [1, 2, "x"] + list(range(3, 11)) + list(range(12, 19)) + ["y", 20]
    )])
    diff = _gofix_diff_header("main.go") + "".join([
        "@@ -1,4 +1,5 @@\n",
        " 1\n",
        " 2\n",
        "+x\n",
        " 3\n",
        " 4\n",
        "@@ -8,7 +9,6 @@\n",
        " 8\n",
        " 9\n",
        " 10\n",
        "-11\n",
        " 12\n",
        " 13\n",
        " 14\n",
        # A count of 1 is omitted.
        "@@ -19 +18 @@\n",
        "-19\n",
        "+y\n",
    ])
    res = testing.run(
        gofix,
        files = {"main.go": original, "other.go": "package main\n"},
        exec_mocks = _gofix_mocks(
            json_out = _gofix_json("main.go", [
                {"posn": "2:1", "end": "2:2", "message": "add x"},
                {"posn": "11:1", "end": "11:1", "message": "remove 11"},
                {"posn": "11:1", "end": "11:1", "message": "really remove 11"},
                # Diagnostics outside of all hunks are ignored.
                {"posn": "6:1", "end": "6:2", "message": "conflicting fix"},
            ]) + _gofix_json("other.go", [
                # Diagnostics for files that `go fix` doesn't change, such as
                # generated files, are ignored.
                {"posn": "1:1", "message": "ignored"},
            ]),
            diff_out = diff,
        ),
    )
    asserts.eq(
        res.findings,
        (
            testing.finding(
                filepath = "main.go",
                line = 1,
                end_line = 4,
                level = "error",
                message = "add x",
                replacements = ("1\n2\nx\n3\n4\n",),
            ),
            testing.finding(
                filepath = "main.go",
                line = 8,
                end_line = 14,
                level = "error",
                message = "remove 11\nreally remove 11",
                replacements = ("8\n9\n10\n12\n13\n14\n",),
            ),
            testing.finding(
                filepath = "main.go",
                line = 19,
                end_line = 19,
                level = "error",
                message = _GOFIX_SUPPORTING_MESSAGE,
                replacements = ("y\n",),
            ),
        ),
    )
    asserts.eq(res.files["main.go"], want_fixed)

def test_gofix_diff_edge_cases():
    diff = "".join([
        _gofix_diff_header("a.go"),
        "@@ -1,3 +1,3 @@\n",
        " a\n",
        # A removed line that looks like a file header.
        "--- b\n",
        "+++ c\n",
        " d\n",
        _gofix_diff_header("b.go"),
        "@@ -1,2 +1,2 @@\n",
        " x\n",
        "-y\n",
        "\\ No newline at end of file\n",
        "+z\n",
    ])
    res = testing.run(
        gofix,
        files = {"a.go": "a\n-- b\nd\n", "b.go": "x\ny"},
        exec_mocks = _gofix_mocks(diff_out = diff),
    )
    asserts.eq(
        [(f.filepath, f.line, f.end_line, f.replacements) for f in res.findings],
        [
            ("a.go", 1, 3, ("a\n++ c\nd\n",)),
            ("b.go", 1, 2, ("x\nz\n",)),
        ],
    )
    asserts.eq(res.files["a.go"], "a\n++ c\nd\n")
    asserts.eq(res.files["b.go"], "x\nz\n")

def test_gofix_truncated_diff():
    asserts.fails(
        lambda: testing.run(
            gofix,
            files = {"a.go": "a\nb\n"},
            exec_mocks = _gofix_mocks(diff_out = _gofix_diff_header("a.go") + "@@ -1,2 +1,2 @@\n a\n"),
        ),
        "truncated `go fix -diff` output",
    )

def test_gofix_unrecognized_position():
    asserts.fails(
        lambda: testing.run(
            gofix,
            files = {"main.go": "package main\n"},
            exec_mocks = _gofix_mocks(
                json_out = json.encode({"example.com/pkg": {"any": [{"posn": "/elsewhere/main.go:1:1", "message": "m"}]}}),
            ),
        ),
        "unrecognized position",
    )

def test_gofix_analyzer_error():
    asserts.fails(
        lambda: testing.run(
            gofix,
            files = {"main.go": "package main\n"},
            exec_mocks = _gofix_mocks(
                json_out = json.encode({"example.com/pkg": {"any": {"error": "boom"}}}) + "\n",
            ),
        ),
        "`go fix` analyzer any failed: boom",
    )
