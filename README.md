# shac

Shac (Scalable Hermetic Analysis and Checks) is a unified and ergonomic tool and
framework for writing and running static analysis checks.

Shac checks are written in [Starlark](https://bazel.build/rules/language).

<!--
GIF generated using https://github.com/asciinema/asciinema and
https://github.com/asciinema/agg.

1. `asciinema rec demo.cast`
  2. (in subshell) `shac check`
  3. (in subshell) Ctrl-D
4. Manually delete the last three lines of `demo.cast` corresponding to the Ctrl-D.
5. `agg --rows 25 --cols 88 --last-frame-duration 10 --font-size 48 demo.cast images/demo.gif`
-->
![usage demonstration](images/demo.gif)

## Usage

```shell
go install go.fuchsia.dev/shac-project/shac@latest
shac check
shac doc shac.star | less
```

## Documentation

* [doc/stdlib.md](doc/stdlib.md): shac runtime standard library documentation.
* [doc/stdlib.star](doc/stdlib.star): shac runtime standard library starlark
  pseudo code.

## Contributing

⚠ The source of truth is at
<https://fuchsia.googlesource.com/shac-project/shac.git> and uses
[Gerrit](https://fuchsia-review.googlesource.com/q/repo:shac-project/shac)
for code review.

See [CONTRIBUTING.md](CONTRIBUTING.md) to submit changes.
