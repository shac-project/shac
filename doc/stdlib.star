# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# This file contains pseudo-code that represents shac's runtime standard
# library solely for documentation purpose.

"""Shac runtime standard library

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
"""

# Note: The shac runtime standard library is implemented in native Go.


## Methods inside ctx object.


def _ctx_emit_annotation(level, message, filepath = None, span = None, replacements = None):
  """Emits an annotation from the current check.

  Example:
    ```python
    def cb(ctx):
      for path, _ in ctx.scm.affected_files().items():
        ctx.emit.annotation(
            level="notice",
            message="great code",
            filepath=path,
            span=((1, 1),),
        )

    shac.register_check(cb)
    ```

  Args:
    level: One of "notice", "warning" or "error".
    message: Message of the annotation.
    filepath: (optional) Path to the source file to annotate.
    span: (optional) One or two pairs of (line,col) tuples that delimits the
      start and the end of the annotation.
    replacements: (optional) List of possible replacements.
  """
  pass


def _ctx_emit_artifact(filepath, content = None):
  """Emits an artifact from the current check.

  Example:
    ```python
    def cb(ctx):
      ctx.emit.artifact("result.txt", "fake data")

    shac.register_check(cb)
    ```

  Args:
    filepath: File name of the artifact. The path must be relative and in POSIX
      format, using / separator.
    content: (optional) Content. If content is omitted, the content of the file
      at filepath will be saved as an artifact.
  """
  pass


def _ctx_io_read_file(filepath, size = None):
  """Returns the content of a file.

  Example:
    ```python
    def cb(ctx):
      # Read at most 4Kib of "path/to/file.txt".
      content = str(ctx.io_read_file("path/to/file.txt", size=4096))
      # Usually run a regexp via ctx.re.match(), or other simple text
      # processing.
      print(content)

    shac.register_check(cb)
    ```

  Args:
    filepath: Path of the file to read. The file must be within the workspace.
      The path must be relative and in POSIX format, using / separator.
    size: (optional) Limits the maximum number of bytes to return. The whole
      file is buffered in memory. Defaults to 128Mib on 32 bits runtime, 4Gib on
      64 bits runtime.

  Returns:
    Content of the file as bytes.
  """
  pass


def _ctx_os_exec(cmd, cwd = None):
  """Runs a command as a subprocess.

  Example:
    ```python
    def cb(ctx):
      if ctx.os.exec(["echo", "hello world"], cwd="."):
        fail("echo failed")

    shac.register_check(cb)
    ```

  Args:
    cmd: Subprocess command line.
    cwd: (optional) Relative path to cwd for the subprocess. Defaults to the
      directory containing shac.star.

  Returns:
    An integer corresponding to the subprocess exit code.
  """
  pass


def _ctx_re_allmatches(pattern, string):
  """Returns all the matches of the regexp pattern onto content.

  Example:
    ```python
    def cb(ctx):
      content = str(ctx.io_read_file("path/to/file.txt"))
      for match in ctx.re.allmatches("TODO\\(([^)]+)\\).*", content):
        print(match)

    shac.register_check(cb)
    ```

  Args:
    pattern: Regexp to run. The syntax as described at
      https://golang.org/s/re2syntax.
    string: String to run the regexp on.

  Returns:
    list(struct(offset=bytes_offset, groups=list(matches)))
  """
  pass


def _ctx_re_match(pattern, string):
  """Returns the first match of the regexp pattern onto content.

  Example:
    ```python
    def cb(ctx):
      content = str(ctx.io_read_file("path/to/file.txt"))
      # Only print the first match, if any.
      match = ctx.re.match("TODO\\(([^)]+)\\).*", "content/true")
      print(match)

    shac.register_check(cb)
    ```

  Args:
    pattern: Pegexp to run. The syntax as described at
      https://golang.org/s/re2syntax.
    string: String to run the regexp on.

  Returns:
    struct(offset=bytes_offset, groups=list(matches))
  """
  pass


def _ctx_scm_affected_files(glob = None):
  """Returns affected files as determined by the SCM.

  If shac detected that the tree is managed by a source control management
  system, e.g. git, it will detect the upstream branch and return only the files
  currently modified.

  If the current directory is not controlled by a SCM, the result is equivalent
  to ctx.scm.all_files().

  If shac is run with the --all options, all files are considered "added" to do
  a full run on all files.

  Example:
    ```python
    def new_todos(cb):
      # Prints only the TODO that were added compared to upstream.
      for path, meta in ctx.scm.affected_files().items():
        for num, line in meta.new_lines():
          m = ctx.re.match("TODO\\(([^)]+)\\).*", line)
          print(path + "(" + str(num) + "): " + m.groups[0])

    shac.register_check(new_todos)
    ```

  Args:
    glob: (optional) TODO: Will later accept a glob.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_line().
  """
  pass


def _ctx_scm_all_files(glob = None):
  """Returns all files found in the current workspace.

  It considers all files "added".

  Example:
    ```python
    def all_todos(cb):
      for path, meta in ctx.scm.all_files().items():
        for num, line in meta.new_lines():
          m = ctx.re.match("TODO\\(([^)]+)\\).*", line)
          print(path + "(" + str(num) + "): " + m.groups[0])

    shac.register_check(all_todos)
    ```

  Args:
    glob: (optional) TODO: Will later accept a glob.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_line().
  """
  pass


def _not_implemented():
  """Not implemented."""
  pass


# ctx is the object passed to shac.register_check(...) callback.
ctx = struct(
  # ctx.emit is the object that exposes the API to emit results for checks.
  emit = struct(
    annotation = _ctx_emit_annotation,
    artifact = _ctx_emit_artifact,
  ),
  # ctx.io is the object that exposes the API to interact with the file system.
  io = struct(
    read_file = _ctx_io_read_file,
  ),
  # ctx.io is the object that exposes the API to interact with the operating
  # system.
  os = struct(
    exec = _ctx_os_exec,
  ),
  # ctx.re is the object that exposes the API to run regular expressions on
  # starlark strings.
  re = struct(
    allmatches = _ctx_re_allmatches,
    match = _ctx_re_match,
  ),
  # ctx.scm is the object exposes the API to query the source control
  # management (e.g. git).
  scm = struct(
    affected_files = _ctx_scm_affected_files,
    all_files = _ctx_scm_all_files,
  ),
)


def dir(x):
  """Starlark builtin that returns all the attributes of an object.

  Primarily used to explore and debug a starlark file.

  See the official documentation at
  https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#dir.

  Args:
    x: Object that has its properties enumerated.

  Example:
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

  Returns:
    List of x object properties as strings. You can use getattr() to retrieve
    each attributes in a loop.
  """
  pass


def fail(*args, sep = " "):
  """Starlark builtin that fails immediately the execution.

  This function will abort execution. When called in the first phase, outside a
  check callback, it will prevent execution of checks.

  When called within a check, it stops the check execution and annotates it with
  an abnormal failure. It also prevents checks that were not yet started from
  running.

  See the official documentation at
  https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#fail.

  Example:
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

  Args:
    *args: Arguments to print out.
    sep: (optional) Separator between the items in args. Defaults to " ".
  """
  pass


## Methods inside the json object.


def _json_decode(x):
  """Decodes a JSON encoded string into the Starlark value that the string
  denotes.

  Supported types include null, bool, int, float, str, dict and list.

  See the full documentation at https://bazel.build/rules/lib/json#decode.

  Example:
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

  Args:
    x: String or bytes of JSON encoded data to convert back to starlark.
  """
  pass


def _json_encode(x):
  """Encodes the starlark value into a JSON encoded string.

  Supported types include null, bool, int, float, str, dict, list and struct.

  See the full documentation at https://bazel.build/rules/lib/json#encode.

  Example:
    ```python
    config = struct(
      foo = "bar",
    )
    print(json.encode(config))
    ```

  Args:
    x: Starlark value to encode to a JSON encoded string.
  """
  pass


def _json_indent(s, *, prefix = "", indent = "\t"):
  """Returns the indented form of a valid JSON-encoded string.

  See the full documentation at https://bazel.build/rules/lib/json#indent.

  Example:
    ```python
    config = struct(
      foo = "bar",
    )
    d = json.encode(config)
    print(json.indent(d))
    ```

  Args:
    s: String or bytes of JSON encoded data to reformat.
    prefix: (optional) Prefix for each new line. Defaults to "".
    indent: (optional) Indent for nested fields. Defaults to "\t".
  """
  pass


# json is a global module that exposes json functions.
#
# The documentation here is listed as a struct instead of a module. The two are
# functionally equivalent.
#
# The implementation matches the official bazel's documentation at
# https://bazel.build/rules/lib/json except that encode_indent is not
# implemented.
json = struct(
  decode = _json_decode,
  encode = _json_encode,
  indent = _json_indent,
)


# It is illegal to have a function named load(). Use a hack that the document
# processor detects to rename the function from load_() to load().
def load_(module, *symbols, **kwsymbols):
  """Starlark builtin that loads an additional shac starlark package and make
  symbols (var, struct, functions) from this file accessible.

  See the official documentation at
  https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#name-binding-and-variables
  and at
  https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#load-statements.

  After a starlark module is loaded, its values are frozen as described at
  https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#freezing-a-value.

  Example:
    ```python
    load("go.star", "gosec")

    def _gosec(ctx):
      # Use a specific gosec version, instead of upstream's default version.
      gosec(ctx, version="v2.9.6")

    register_checks(_gosec)
    ```

  Args:
    module: Path to a local module to load. In the future, a remote path will be
      allowed.
    *symbols: Symbols to load from the module.
    **kwsymbols: Symbols to load from the module that will be accessible under a
      new name.
  """
  pass


def print(*args, sep = " "):
  """Starlark builtin that prints a debug log.

  This function should only be used while debugging the starlark code.

  Example:
    ```python
    print("shac", "is", "great")
    ```

  See the official documentation at
  https://github.com/google/starlark-go/blob/HEAD/doc/spec.md#print.

  Args:
    args: Arguments to print out.
    sep: (optional) Separator between the items in args. Defaults to " ".
  """
  pass


## Methods inside the shac object.


def _shac_register_check(callback):
  """Registers a shac check.

  It must be called at least once for the starlark file be a valid check file.
  Each callback will be run in parallel.

  Example:
    ```python
    def cb(ctx):
      fail("implement me")

    shac.register_check(cb)
    ```

  Args:
    callback: Starlark function that is called back to implement the check. The
      callback must accept one ctx(...) argument and return None.
  """
  pass


# shac is the global available at runtime when loading your starlark code.
shac = struct(
  # The git hash of shac.git where shac was built.
  commit_hash = "<hash>",
  register_check = _shac_register_check,
  # The semver version number of shac.
  version = (0, 0, 1),
)


def struct_():
  """Creates and return a structure instance.

  This a non-standard function that enables creating an "object" that has
  immutable properties. It is intentionally not as powerful as a python class
  instance.

  Example:
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

  Args:
    **kwargs: structure's fields.
  """
  pass
