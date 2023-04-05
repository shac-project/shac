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

- io
- os
- re
- result
- scm

## ctx.io

ctx.io is the object that exposes the API to interact with the file system.

Fields:

- read_file

## ctx.io.read_file

Returns the content of a file.

### Example

```python
def cb(ctx):
  content = str(ctx.io_read_file("path/to/file.txt"))
  # Usually run a regexp via ctx.re.match(), or other simple text
  # processing.
  print(content)

shac.register_check(cb)
```

### Arguments

* **path**: path of the file to read. The file must be within the workspace. The path must be relative and in POSIX format, using / separator.

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
* **cwd**: Relative path to cwd for the subprocess.

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

* **pattern**: regexp to run. It must use the syntax as described at https://golang.org/s/re2syntax.
* **str**: string to run the regexp on.

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

* **pattern**: regexp to run. It must use the syntax as described at https://golang.org/s/re2syntax.
* **str**: string to run the regexp on.

### Returns

struct(offset=bytes_offset, groups=list(matches))

## ctx.result

ctx.result is the object that exposes the API to emit results for checks.

Fields:

- emit_comment
- emit_row
- emit_artifact

## ctx.result.emit_comment

Not implemented.


## ctx.result.emit_row

Not implemented.


## ctx.result.emit_artifact

Not implemented.


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

* **glob**: TODO: Will later accept a glob.

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

* **glob**: TODO: Will later accept a glob.

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

* **x**: object that will have its properties enumerated.

### Returns

list of x object properties as strings. You can use getattr() to retrieve
each attributes in a loop.

## fail

Starlark builtin that fails immediately the execution.

This function should not be used normally. It can be used as a quick debugging
tool or when there is an irrecoverable failure that should immediately stop
all execution.

See the official documentation at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#fail.

### Example

```python
fail("implement me")
```

### Arguments

* **\*args**: arguments to print out.
* **sep**: separator between the items in args, defaults to " ".

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

* **x**: string or bytes of JSON encoded data to convert back to starlark.

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

* **x**: starlark value to encode to a JSON encoded string.

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

* **x**: string or bytes of JSON encoded data to reformat.
* **prefix**: prefix for each new line.
* **indent**: indent for nested fields.

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

* **module**: path to a local module to load. In the future, a remote path will be allowed.
* **\*symbols**: symbols to load from the module.
* **\*\*kwsymbols**: symbols to load from the module that will be accessible under a new name.

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

* **args**: arguments to print out.
* **sep**: separator between the items in args, defaults to " ".

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

* **cb**: Starlark function that is called back to implement the check. Passed a single argument ctx(...).

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
