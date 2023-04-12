# Shac runtime standard library

Shac uses the starlark language. Starlark is a python derivative.
https://bazel.build/rules/language is a great resource if the language is new to
you, just ignore the bazel references. The starlark language formal
specification is documented at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md.

While all [starlark-go's built-in constants and functions are
available](https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#built-in-constants-and-functions),
a few are explicitly documented here to highlight them.

These [experimental
features](https://pkg.go.dev/go.starlark.net/resolve#pkg-variables) are enabled:

- AllowSet: "set" built-in is enabled.
- AllowRecursion: allow while statements and recursion. This allows potentially
  unbounded runtime.

## Table of contents

- [ctx](#ctx)
- [dir](#dir)
- [fail](#fail)
- [json](#json)
- [load](#load)
- [print](#print)
- [shac](#shac)
- [struct](#struct)

## ctx

ctx is the object passed to [shac.register_check(...)](#shac.register-check) callback.

Fields:

- emit
- io
- os
- re
- scm

## ctx.emit

ctx.emit is the object that exposes the API to emit results for checks.

Fields:

- annotation
- artifact

## ctx.emit.annotation

Emits an annotation from the current check.

### Example

A check level annotation:

```python
def cb(ctx):
  ctx.emit.annotation(level="warning", message="Do not change anything")

shac.register_check(cb)
```

An annotation associated with a specific file:

```python
def cb(ctx):
  for path, _ in ctx.scm.affected_files().items():
    ctx.emit.annotation(
        level="notice",
        message="great code",
        filepath=path,
    )

shac.register_check(cb)
```

An annotation associated with a specific span within a file:

```python
def cb(ctx):
  for path, meta in ctx.scm.affected_files().items():
    for num, line in meta.new_lines():
      ctx.emit.annotation(
          level="error",
          message="This line is superfluous",
          filepath=path,
          line=num,
          col=1,
          end_line=num,
          end_col=len(line))
      )

shac.register_check(cb)
```

### Arguments

* **level**: One of "notice", "warning" or "error".
* **message**: Message of the annotation.
* **filepath**: (optional) Path to the source file to annotate.
* **line**: (optional) Line where the annotation should start. 1 based.
* **col**: (optional) Column where the annotation should start. 1 based.
* **end_line**: (optional) Line where the annotation should end if it represents a span. 1 based.
* **end_col**: (optional) Column where the annotation should end if it represents a span. 1 based.
* **replacements**: (optional) A sequence of str, representing possible replacement suggestions. The sequence can be a list or a tuple.

## ctx.emit.artifact

Emits an artifact from the current check.

### Example

```python
def cb(ctx):
  ctx.emit.artifact("result.txt", "fake data")

shac.register_check(cb)
```

### Arguments

* **filepath**: File name of the artifact. The path must be relative and in POSIX format, using / separator.
* **content**: (optional) Content. If content is omitted, the content of the file at filepath will be saved as an artifact.

## ctx.io

ctx.io is the object that exposes the API to interact with the file system.

Fields:

- read_file

## ctx.io.read_file

Returns the content of a file.

### Example

```python
def cb(ctx):
  # Read at most 4Kib of "path/to/file.txt".
  content = str(ctx.io_read_file("path/to/file.txt", size=4096))
  # Usually run a regexp via ctx.re.match(), or other simple text
  # processing.
  print(content)

shac.register_check(cb)
```

### Arguments

* **filepath**: Path of the file to read. The file must be within the workspace. The path must be relative and in POSIX format, using / separator.
* **size**: (optional) Limits the maximum number of bytes to return. The whole file is buffered in memory. Defaults to 128Mib on 32 bits runtime, 4Gib on 64 bits runtime.

### Returns

Content of the file as bytes.

## ctx.os

ctx.io is the object that exposes the API to interact with the operating
system.

Fields:

- exec

## ctx.os.exec

Runs a command as a subprocess.

### Example

```python
def cb(ctx):
  if ctx.os.exec(["echo", "hello world"], cwd="."):
    fail("echo failed")

shac.register_check(cb)
```

### Arguments

* **cmd**: Subprocess command line.
* **cwd**: (optional) Relative path to cwd for the subprocess. Defaults to the directory containing shac.star.

### Returns

An integer corresponding to the subprocess exit code.

## ctx.re

ctx.re is the object that exposes the API to run regular expressions on
starlark strings.

Fields:

- allmatches
- match

## ctx.re.allmatches

Returns all the matches of the regexp pattern onto content.

### Example

```python
def cb(ctx):
  content = str(ctx.io_read_file("path/to/file.txt"))
  for match in ctx.re.allmatches("TODO\(([^)]+)\).*", content):
    print(match)

shac.register_check(cb)
```

### Arguments

* **pattern**: Regexp to run. The syntax as described at https://golang.org/s/re2syntax.
* **string**: String to run the regexp on.

### Returns

list(struct(offset=bytes_offset, groups=list(matches)))

## ctx.re.match

Returns the first match of the regexp pattern onto content.

### Example

```python
def cb(ctx):
  content = str(ctx.io_read_file("path/to/file.txt"))
  # Only print the first match, if any.
  match = ctx.re.match("TODO\(([^)]+)\).*", "content/true")
  print(match)

shac.register_check(cb)
```

### Arguments

* **pattern**: Pegexp to run. The syntax as described at https://golang.org/s/re2syntax.
* **string**: String to run the regexp on.

### Returns

struct(offset=bytes_offset, groups=list(matches))

## ctx.scm

ctx.scm is the object exposes the API to query the source control
management (e.g. git).

Fields:

- affected_files
- all_files

## ctx.scm.affected_files

Returns affected files as determined by the SCM.

If shac detected that the tree is managed by a source control management
system, e.g. git, it will detect the upstream branch and return only the files
currently modified.

If the current directory is not controlled by a SCM, the result is equivalent
to ctx.scm.all_files().

If shac is run with the --all options, all files are considered "added" to do
a full run on all files.

### Example

```python
def new_todos(cb):
  # Prints only the TODO that were added compared to upstream.
  for path, meta in ctx.scm.affected_files().items():
    for num, line in meta.new_lines():
      m = ctx.re.match("TODO\(([^)]+)\).*", line)
      print(path + "(" + str(num) + "): " + m.groups[0])

shac.register_check(new_todos)
```

### Arguments

* **glob**: (optional) TODO: Will later accept a glob.

### Returns

A map of {path: struct()} where the struct has a string field action and a
function new_line().

## ctx.scm.all_files

Returns all files found in the current workspace.

It considers all files "added".

### Example

```python
def all_todos(cb):
  for path, meta in ctx.scm.all_files().items():
    for num, line in meta.new_lines():
      m = ctx.re.match("TODO\(([^)]+)\).*", line)
      print(path + "(" + str(num) + "): " + m.groups[0])

shac.register_check(all_todos)
```

### Arguments

* **glob**: (optional) TODO: Will later accept a glob.

### Returns

A map of {path: struct()} where the struct has a string field action and a
function new_line().

## dir

Starlark builtin that returns all the attributes of an object.

Primarily used to explore and debug a starlark file.

See the official documentation at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#dir.

### Example

```python
def print_attributes(name, obj):
  for attrname in dir(obj):
    attrval = getattr(obj, attrname)
    attrtype = type(attrval)
    fullname = name + "." + attrname
    if attrtype in ("builtin_function_or_method", "function"):
      print(fullname + "()")
    elif attrtype == "struct":
      print_attributes(fullname, attrval)
    else:
      print(fullname + "=" + repr(attrval))

def cb(ctx):
  print_attributes("ctx", ctx)
  print_attributes("str", "")
  print_attributes("dict", {})
  print_attributes("set", set())
  print_attributes("struct", struct(foo = "bar", p = print_attributes))

shac.register_check(cb)
```

### Arguments

* **x**: Object that has its properties enumerated.

### Returns

List of x object properties as strings. You can use getattr() to retrieve
each attributes in a loop.

## fail

Starlark builtin that fails immediately the execution.

This function will abort execution. When called in the first phase, outside a
check callback, it will prevent execution of checks.

When called within a check, it stops the check execution and annotates it with
an abnormal failure. It also prevents checks that were not yet started from
running.

See the official documentation at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#fail.

### Example

The code after the fail() call will not be executed:

```python
fail("implement me")
```

The check will be annotated with the abnormal failure:

```python
def cb1(ctx):
  fail("implement me")

def cb2(ctx):
  # This check may or may not run, depending on concurrency.
  pass

shac.register_check(cb1)
shac.register_check(cb2)
```

### Arguments

* **\*args**: Arguments to print out.
* **sep**: (optional) Separator between the items in args. Defaults to " ".

## json

json is a global module that exposes json functions.

The documentation here is listed as a struct instead of a module. The two are
functionally equivalent.

The implementation matches the official bazel's documentation at
https://bazel.build/rules/lib/json except that encode_indent is not
implemented.

Fields:

- decode
- encode
- indent

## json.decode

Decodes a JSON encoded string into the Starlark value that the string
denotes.

Supported types include null, bool, int, float, str, dict and list.

See the full documentation at https://bazel.build/rules/lib/json#decode.

### Example

```python
data = json.decode('{"foo":"bar}')
print(data["foo"])

def cb(ctx):
  # Load a configuration from a json file in the tree, containing a
  # dict with a "version" key.
  decoded = ctx.io.read_file("config.json")
  print(decoded["version"])

shac.register_check(cb)
```

### Arguments

* **x**: String or bytes of JSON encoded data to convert back to starlark.

## json.encode

Encodes the starlark value into a JSON encoded string.

Supported types include null, bool, int, float, str, dict, list and struct.

See the full documentation at https://bazel.build/rules/lib/json#encode.

### Example

```python
config = struct(
  foo = "bar",
)
print(json.encode(config))
```

### Arguments

* **x**: Starlark value to encode to a JSON encoded string.

## json.indent

Returns the indented form of a valid JSON-encoded string.

See the full documentation at https://bazel.build/rules/lib/json#indent.

### Example

```python
config = struct(
  foo = "bar",
)
d = json.encode(config)
print(json.indent(d))
```

### Arguments

* **s**: String or bytes of JSON encoded data to reformat.
* **prefix**: (optional) Prefix for each new line. Defaults to "".
* **indent**: (optional) Indent for nested fields. Defaults to "	".

## load

Starlark builtin that loads an additional shac starlark package and make
symbols (var, struct, functions) from this file accessible.

See the official documentation at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#name-binding-and-variables
and at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#load-statements.

After a starlark module is loaded, its values are frozen as described at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#freezing-a-value.

### Example

```python
load("go.star", "gosec")

def _gosec(ctx):
  # Use a specific gosec version, instead of upstream's default version.
  gosec(ctx, version="v2.9.6")

register_checks(_gosec)
```

### Arguments

* **module**: Path to a local module to load. In the future, a remote path will be allowed.
* **\*symbols**: Symbols to load from the module.
* **\*\*kwsymbols**: Symbols to load from the module that will be accessible under a new name.

## print

Starlark builtin that prints a debug log.

This function should only be used while debugging the starlark code.
See the official documentation at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#print.

### Example

```python
print("shac", "is", "great")
```

### Arguments

* **\*args**: Arguments to print out.
* **sep**: (optional) Separator between the items in args. Defaults to " ".

## shac

shac is the global available at runtime when loading your starlark code.

Fields:

- commit_hash
- register_check
- version

## shac.commit_hash

The git hash of shac.git where shac was built.

## shac.version

The semver version number of shac.

## shac.register_check

Registers a shac check.

It must be called at least once for the starlark file be a valid check file.
Each callback will be run in parallel.

### Example

```python
def cb(ctx):
  fail("implement me")

shac.register_check(cb)
```

### Arguments

* **callback**: Starlark function that is called back to implement the check. The callback must accept one ctx(...) argument and return None.

## struct

Creates and return a structure instance.

This a non-standard function that enables creating an "object" that has
immutable properties. It is intentionally not as powerful as a python class
instance.

### Example

```python
def _do():
  print("it works")

obj = struct(
  value = "a value",
  do = _do,
)

print(obj.value)
obj.do()
```

### Arguments

* **\*\*kwargs**: structure's fields.
