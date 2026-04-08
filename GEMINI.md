# GEMINI.md

## Repository Overview

`shac` (Scalable Hermetic Analysis and Checks) is a unified tool and framework
for writing and running static analysis checks. Checks are written in Starlark.

## Codebase Structure

- `main.go`: The entry point for the `shac` CLI.
- `internal/`: Contains the core implementation of `shac`, including the engine
  and SCM integration.
- `checks/`: Contains Starlark check definitions loaded by the main `shac.star`
  file.
- `scripts/`: Contains helper scripts.
  - `tests.sh`: The main test script that runs tests, linting, and benchmarks.

## Common Workflows

### Testing

To run all tests, including coverage and benchmarks, use the provided script:

```shell
./scripts/tests.sh
```

For faster iteration you can run just the standard Go tests:

```shell
go test ./...
```

### Linting and Formatting

This repository dogfoods `shac` for its own checks.

To run checks on affected files:

```shell
go run . check
```

To automatically apply fixes (formatting):

```shell
go run . fmt
```

### Version Bumps

For user-visible changes or API changes, consider updating the version in
`internal/engine/version.go`.

### Guiding Principles for Agents

- **Dogfooding**: Always run `go run . check` to verify your changes against the
  project's own checks.
- **CGO**: Testing scripts disable CGO (`CGO_ENABLED=0`). Keep this in mind if
  you encounter platform-specific issues.
