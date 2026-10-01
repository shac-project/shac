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

load("go.star", "gofmt", "gosec", "govet", "ineffassign", "no_fork_without_lock", "shadow", "staticcheck")

def _go_install_mock(pkg, version):
    return testing.exec_mock(
        cmd = ["go", "install", "%s@%s" % (pkg, version)],
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
            _go_install_mock("github.com/securego/gosec/v2/cmd/gosec", "v2.22.3"),
            testing.exec_mock(
                cmd = [
                    ".tools/gobin/gosec",
                    "-fmt=json",
                    "-quiet",
                    "-exclude=G204,G304",
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
            _go_install_mock("github.com/securego/gosec/v2/cmd/gosec", "v2.22.3"),
            testing.exec_mock(
                cmd = [
                    ".tools/gobin/gosec",
                    "-fmt=json",
                    "-quiet",
                    "-exclude=G204,G304",
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
            _go_install_mock("github.com/gordonklaus/ineffassign", "v0.0.0-20230107090616-13ace0543b28"),
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
            _go_install_mock("github.com/gordonklaus/ineffassign", "v0.0.0-20230107090616-13ace0543b28"),
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
            _go_install_mock("honnef.co/go/tools/cmd/staticcheck", "v0.4.3"),
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
            _go_install_mock("honnef.co/go/tools/cmd/staticcheck", "v0.4.3"),
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
            _go_install_mock("golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow", "v0.31.0"),
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
            _go_install_mock("golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow", "v0.31.0"),
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
                stderr = "# example.com/pkg\n{}\n",
            ),
        ],
    )
    asserts.eq(res.findings, ())

def test_govet_fail():
    stderr = "\n".join([
        "warning: GOPATH set to GOROOT",
        "# example.com/pkg",
        json.encode({
            "example.com/pkg": {
                "copylocks": [
                    {
                        "posn": testing.root + "/main.go:30:12",
                        "message": "call of foo copies lock value: sync.Mutex",
                    },
                ],
            },
        }),
    ])
    res = testing.run(
        govet,
        files = {"main.go": "package main\n"},
        exec_mocks = [
            testing.exec_mock(
                cmd = ["go", "vet", "-json", "-copylocks", "-unusedresult", "./..."],
                stderr = stderr,
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
