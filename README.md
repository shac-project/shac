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

## Documentation

- [doc/design.md](doc/design.md): High-level design information.
- [doc/stdlib.md](doc/stdlib.md): shac runtime standard library documentation.
- [doc/stdlib.star](doc/stdlib.star): shac runtime standard library starlark
  pseudo code.

## Getting started

The simplest way to install shac is with `go install`:

```shell
go install go.fuchsia.dev/shac-project/shac@latest
```

### Simple text-based checks

To start introducing shac checks in a repository, create a `shac.star` file in
the root of the repository:

```python
print("hello, world from shac!")
```

Now run `shac check`:

```shell
$ shac check
[//shac.star:1] hello, world from shac!
```

This is the simplest possible `shac.star` file; it doesn't run any checks, just
executes a single print statement.

Let's start implementing some checks. Update the contents of `shac.star` to the
following:

```python
def no_trailing_whitespace(ctx):
    """Check that no source files contain lines with trailing whitespace."""
    for f in ctx.scm.affected_files():
        contents = str(ctx.io.read_file(f))
        for line in contents.splitlines():
            if line.endswith(" "):
                fail("line in %s has trailing whitespace" % f)

shac.register_check(no_trailing_whitespace)
```

Starlark code, including shac's dialect of Starlark, looks a lot like Python.
However, shac introduces some additional domain-specific functionality on top of
vanilla Starlark. Let's go over the shac-specific features from this code snippet:

- [`shac.register_check()`](doc/stdlib.md#shac_register_check) is the main
  entrypoint for declaring checks that shac should run. It accepts a function
  that implements the check logic (or a [`shac.check()`](doc/stdlib.md#check)
  object for more advanced use cases). The function that implements the check
  must accept a single `ctx` object, which is the entrypoint to most shac
  standard library functionality for interacting with the OS and filesystem and
  emitting check results.

- [`ctx.scm.affected_files()`](doc/stdlib.md#ctx_scm_affected_files) queries the
  source control management system (likely Git) to determine the names of files
  that differ from upstream. Most shac checks will call
  `ctx.scm.affected_files()` to determine the files to analyze.  This means
  `shac check` will mostly only analyze changed files by default, since
  unchanged files are less likely to contain relevant problems, and analyzing
  the entire repository would add significant latency.
  `ctx.scm.affected_files()` returns a dict with keys that are source-relative
  paths, e.g. "path/to/foo.py".

- [`ctx.io.read_file(f)`](doc/stdlib.md#ctx_io_read_file) is the method to read
  a file from disk. If given a relative path, it assumes the path is relative to
  the directory containing the shac.star file. It returns a `bytes` object.

- [`fail()`](doc/stdlib.md#fail) is the Starlark equivalent of raising an
  exception, but there's no way to recover from `fail()` calls. Calling `fail()`
  is the simplest way to cause shac to fail when a check encounters an error,
  but it's not very elegant because it can only be called once and doesn't
  natively support emitting any sort of structured data about the error (e.g.
  file name and line number that triggered the error).

Now if you run `shac check` again, it should print the following as long as you
don't have any modified files with trailing whitespace:

```shell
$ shac check
- no_trailing_whitespace (success in 20ms)
```

Now let's intentionally break the check to make sure it works:

```shell
$ echo "this line has trailing whitespace    " > trailing_whitespace.txt
$ shac check
- no_trailing_whitespace (error in 21ms): fail: line in trailing_whitespace.txt has trailing whitespace
Traceback (most recent call last):
  //shac.star:6:21: in no_trailing_whitespace
shac: fail: line in trailing_whitespace.txt has trailing whitespace
```

This is great and all, but it doesn't tell us *which* line has trailing
whitespace, and only produces a single error message even if multiple files
and/or multiple lines contain trailing whitespace.

To fail more gracefully with more structured data, we can use the
[`ctx.emit.finding()`](doc/stdlib.md#ctx_emit_finding) function:

```python
def no_trailing_whitespace(ctx):
    """Check that no source files contain lines with trailing whitespace."""
    for f in ctx.scm.affected_files():
        contents = str(ctx.io.read_file(f))
        for i, line in enumerate(contents.splitlines()):
            stripped = line.rstrip()
            if stripped != line:
                ctx.emit.finding(
                    level = "error",
                    filepath = f,
                    # Finding lines and columns are 1-indexed.
                    line = i + 1,
                    col = len(stripped) + 1,
                    # End column is exclusive.
                    end_col = len(line) + 1,
                    message = "Delete trailing whitespace.",
                    replacements = [""],
                )

shac.register_check(no_trailing_whitespace)
```

`ctx.emit.finding()` emits an error message that's annotated with a filepath and
location within the source file, as well as a severity level. `level = "error"`
causes `shac check` to produce an exit code of 1. The other possible levels,
`"warning"` and `"notice"`, only print the message without causing shac to fail.

Now run `shac check` again:

```shell
$ shac check
[no_trailing_whitespace/error] trailing_whitespace.txt(1): Delete trailing whitespace.

  this line has trailing whitespace


- no_trailing_whitespace (error in 22ms)
```

Much better! Now we can see the exact location where the error occurred, and if
multiple files contain trailing whitespace then we'll see all of them.

### Shelling out to external processes

Many static analysis checks are supported by third-party tools such as
language-specific linters and formatters. Rather than reimplementing the logic
for those tools in shac Starlark code, shac makes it possible to shell out to
external subprocesses using the [`ctx.os.exec()`](doc/stdlib.md#ctx_os_exec)
function.

Let's update `shac.star` to add a check that enforces formatting of Python source
files with `black` (assumes you have [Black](https://github.com/psf/black)
installed locally):

```python
def _black(ctx):
    python_files = [f for f in ctx.scm.affected_files() if f.endswith(".py")]

    for f in python_files:
        # Check if the file is unformatted. If `black --check` produces a
        # retcode of 1, that means it's unformatted.
        res = ctx.os.exec(
            ["black", "--check", f],
            ok_retcodes = (0, 1),
        ).wait()
        if res.retcode == 0:
            continue
        # Get the formatted text so we can tell `shac fmt` how to update the
        # file.
        formatted = ctx.os.exec(
            ["black", "-"],
            stdin = ctx.io.read_file(f),
        ).wait().stdout
        # Note that `message` is not required for formatter checks.
        ctx.emit.finding(
            level = "error",
            filepath = f,
            replacements = [formatted],
        )

black = shac.check(
    _black,
    # Registers the check as a formatter so it gets run by `shac fmt`.
    formatter = True,
)

shac.register_check(black)
```

`ctx.os.exec()` starts a subprocess in the background and returns a process
object with a `.wait()` method that blocks until the process completes, and
returns a completed process object with `stdout`, `stderr`, and `retcode` methods.

By default, if the process produces a non-zero exit code then it will implicitly
call `fail()` and abort the check. This behavior be configured by the
`ok_retcodes` parameter to `ctx.os.exec()`, which declares additional return
codes as "okay", i.e. they shouldn't cause the check to fail.

Note that we are now setting the `replacements` argument to
`ctx.emit.finding()`, which lets us run `shac fmt` to automatically apply fixes
for findings from checks marked as `formatter = True`. Subprocesses run by shac
checks are sandboxed (on Linux) and not allowed to write to the source tree.
Therefore, any fixes can only be declared by calling `ctx.emit.finding()`, and
shac will apply the fixes (as long as `replacements` has only one element) after
all checks have completed.

Now we can run `shac check` to make sure it catches Black-incompliant Python
code:

```shell
$ echo "print(  'foo')" > main.py
$ shac check
[black/error] main.py: File not formatted. Run `shac fmt` to fix.
- black (error in 1.062s)
```

Now run `shac fmt` to apply the fixes emitted by Black:

```shell
$ shac fmt
- black (1 finding to fix)
Fixed 1 issue in main.py
$ cat main.py
print("foo")
```

Now `shac check` should pass:

```shell
$ shac check
- black (success in 540ms)
```

## Road map

Planned features/changes, in descending order by priority:

- [x] Configuring files to exclude from shac analysis in `shac.textproto`
- [x] Include unstaged files in analysis, including respecting unstaged
      `shac.star` files
- [x] Automatic fix application with handling for conflicting suggestions
- [ ] Provide a `.shac` cache directory that checks can write to
- [ ] Mount checkout directory read-only
  - [x] By default
  - [ ] Unconditionally
- [ ] Give checks access to the commit message via `ctx.scm`
- [ ] Built-in formatting of Starlark files
- [ ] Configurable "pass-throughs" - non-default environment variables and
      mounts that can optionally be passed through to the sandbox
- [ ] Add `glob` arguments to `ctx.scm.{all,affected}_files()` functions for
      easier filtering
- [ ] Filesystem sandboxing on MacOS
- [ ] Windows sandboxing
- [ ] Testing framework for checks

## Contributing

⚠ The source of truth is at
<https://fuchsia.googlesource.com/shac-project/shac.git> and uses
[Gerrit](https://fuchsia-review.googlesource.com/q/repo:shac-project/shac)
for code review.

See [CONTRIBUTING.md](CONTRIBUTING.md) to submit changes.
