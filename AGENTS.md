# AGENTS.md

`shac` (Scalable Hermetic Analysis and Checks) is a tool and framework for
writing and running static analysis checks in Starlark. The CLI entry point is
`main.go`, the engine lives in `internal/`, checks used by this repo's own
`shac.star` live in `checks/`, and user docs live in `doc/`.

## Testing and Linting

- `./scripts/tests.sh` runs tests, linting, and benchmarks. `go test ./...` is
  fine for faster iteration.
- This repo dogfoods shac: run `go run . check` (and `go run . fmt` to apply
  fixes) before finishing.
- If your `go` switches toolchains via `GOTOOLCHAIN`, `go run . check` fails
  inside the nsjail sandbox with `Couldn't mount '/usr/bin/go'`. Put the
  selected toolchain first on `PATH` and pin it:

  ```shell
  GOTOOLCHAIN=local PATH="$(GOTOOLCHAIN=go<version> go env GOROOT)/bin:$PATH" go run . check
  ```

- GitHub Actions also run the tests on Windows and macOS, which Gerrit
  presubmit doesn't. In Go tests that execute programs, use `.bat` scripts or
  `cmd.exe /c ...` rather than shell scripts, and add `.exe` to executable
  names on Windows.
- Tests that spawn subprocesses in parallel should use `execsupport.Run`, and
  tests that write executables should hold `execsupport.Mu.Lock()` while
  writing, to avoid `ETXTBSY` fork races (see `internal/execsupport`). The
  `no_fork_without_lock` check doesn't cover test files.
- Unmocked `testing.run` tests in `checks/` that run Go tools should pass an
  otherwise-unused `subdir`. Otherwise this repo's `go.mod` and `vendor/` leak
  into the test module and cause `inconsistent vendoring` errors.

## Documentation

`doc/stdlib.md` is generated from `doc/stdlib.star`. Regenerate it with:

```shell
cd internal/engine && go run docregen_stdlib.go
```

Don't use `go generate ./internal/engine`: it also regenerates `shac.pb.go`
with your local `protoc` and churns its version header.

Docstrings are parsed by LUCI's `docstring.Parse`, which fails silently:

- An `Args:` block only renders as a structured list if **every** unindented
  line is `<name>: <description>`. One stray line (e.g. `arg (optional): ...`
  instead of `arg: (optional) ...`) turns the whole block into free text.
- Docstrings are ordinary Starlark strings, so double the backslashes in
  `Example:` blocks (`"a\\n"` renders as `"a\n"`).

## Versioning and Dependencies

- Bump the patch version in `internal/engine/version.go` once per user-visible
  commit (new API, CLI flag, or behavior change). Internal, test-only, and
  docs-only commits don't need a bump. Stacked user-visible changes each bump
  once, in order.
- When changing the Go version, update every `LINT.IfChange(goversion)` block
  listed in `go.mod`. Nothing enforces this automatically.
- Use `GOWORK=off` when running `go mod tidy` or `go mod vendor`, so a parent
  `go.work` doesn't interfere.
