# Copyright 2023 The Shac Authors
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


def _ctx_emit_finding(level, message, filepath = None, line = None, col = None, end_line = None, end_col = None, replacements = None):
  """Emits a finding from the current check.

  Example:
    A check level finding:

    ```python
    def cb(ctx):
      ctx.emit.finding(level="warning", message="Do not change anything")

    shac.register_check(cb)
    ```

    An finding associated with a specific file:

    ```python
    def cb(ctx):
      for path, _ in ctx.scm.affected_files().items():
        ctx.emit.finding(
            level="notice",
            message="great code",
            filepath=path,
        )

    shac.register_check(cb)
    ```

    An finding associated with a specific span within a file:

    ```python
    def cb(ctx):
      for path, meta in ctx.scm.affected_files().items():
        for num, line in meta.new_lines():
          ctx.emit.finding(
              level="error",
              message="This line is superfluous",
              filepath=path,
              line=num,
              col=1,
              end_line=num,
              end_col=len(line)) + 1,
          )

    shac.register_check(cb)
    ```

  Args:
    level: One of "notice", "warning" or "error".
    message: Message of the finding.
    filepath: (optional) Path to the source file to annotate.
    line: (optional) Line where the finding should start. 1 based.
    col: (optional) Column where the finding should start. 1 based.
    end_line: (optional) Line where the finding should end if it represents a
      span, inclusive. 1 based.
    end_col: (optional) Column where the finding should end if it represents a
      span, exclusive. 1 based.
    replacements: (optional) A sequence of str, representing possible
      replacement suggestions. The sequence can be a list or a tuple. The
      replacements apply to the entire file if no span is specified for the
      finding.
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
    size: (optional) Limits the maximum number of bytes to return. The file is
      silently truncated to this value. The whole file is buffered in memory.
      Defaults to 128Mib on 32 bits runtime, 4Gib on 64 bits runtime.

  Returns:
    Content of the file as bytes.
  """
  pass


def _ctx_io_tempdir():
  """Returns a new temporary directory."""
  pass


def _ctx_os_exec(
  cmd,
  cwd = None,
  env = None,
  allow_network = False,
  ok_retcodes = None,
  raise_on_failure = True,
):
  """Runs a command as a subprocess.

  Subprocesses are denied network access by default on Linux. Use
  `allow_network = True` to grant the subprocess network access.

  Example:
    ```python
    def cb(ctx):
      res = ctx.os.exec(["echo", "hello world"], cwd=".").wait()
      print(res.stdout)  # "hello world"

    shac.register_check(cb)
    ```

    Use `raise_on_failure = False` to prevent a non-zero retcode from
    automatically failing the check:

    ```python
    def cb(ctx):
      res = ctx.os.exec(["cat", "does-not-exist.txt"], raise_on_failure = False).wait()
      print(res.retcode)  # 1
      print(res.stderr)   # cat: does-not-exist.txt: No such file or directory

    shac.register_check(cb)
    ```

    Use `env` to pass environment variables:

    ```python
    def cb(ctx):
      ctx.os.exec(["foo"], env = {"FOO_CONFIG": "foo.config"}).wait()

    shac.register_check(cb)
    ```

  Args:
    cmd: Subprocess command line.
    cwd: (optional) Relative path to cwd for the subprocess. Defaults to the
      directory containing shac.star.
    env: (optional) Dictionary of environment variables to set for the
      subprocess.
    allow_network: (optional) Allow network access. Defaults to false.
    ok_retcodes: (optional) List of exit codes that should be considered
      successes. Any other exit code will immediately fail the check. The
      effective default is [0].
    raise_on_failure: (optional) Whether the running check should automatically
      fail if the subcommand returns a non-zero exit code. Defaults to true.
      Cannot be false if ok_retcodes is also set.

  Returns:
    A subprocess object with a wait() method. wait() returns a
    struct(retcode=..., stdout="...", stderr="...")
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


def _ctx_scm_affected_files(glob = None, include_deleted = False):
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
    include_deleted: (optional) Whether to include deleted files. By default
      deleted files are excluded.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_lines().
  """
  pass


def _ctx_scm_all_files(glob = None, include_deleted = False):
  """Returns all files found in the current workspace.

  All files are considered "added" or "deleted".

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
    include_deleted: (optional) Whether to include deleted files. By default
      deleted files are excluded.

  Returns:
    A map of {path: struct()} where the struct has a string field action and a
    function new_lines().
  """
  pass


# ctx is the object passed to shac.register_check(...) callback.
ctx = struct(
  # ctx.emit is the object that exposes the API to emit results for checks.
  emit = struct(
    finding = _ctx_emit_finding,
    artifact = _ctx_emit_artifact,
  ),
  # ctx.io is the object that exposes the API to interact with the file system.
  io = struct(
    read_file = _ctx_io_read_file,
    tempdir = _ctx_io_tempdir,
  ),
  # ctx.os is the object that exposes the API to interact with the operating
  # system.
  os = struct(
    exec = _ctx_os_exec,
    # ctx.os.name contains the OS as described by GOOS. Frequent values are
    # "windows", "linux" and "darwin". The full exact list can be retrieved with
    # the command "go tool dist list | cut -f 1 -d / | uniq"
    name = "",
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
    # ctx.scm.root is the absolute path to the project root.
    root = "",
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

    There are 5 forms for the module to load:

    1. Loads a file in the same directory as the calling code. In this case,
    ".." is allowed as long as it stays within the root directory of the current
    project:

    ```python
    load("../common/go.star", "gosec")

    register_check(gosec)
    ```

    2. Loads a file relative to the root directory:

    ```python
    load("//common/go.star", "gosec")

    register_check(gosec)
    ```

    3. Loading an external package via a package alias and default api.star.
    This uses the aliases defined in shac.textproto and loads api.star in this
    dependency:

    ```python
    # Implicitly loads api.star
    load("@static-checks", "go")

    register_check(go.gosec)
    ```

    4. Loading an external package via a package alias and a specific file. This
    uses the aliases defined in shac.textproto and loads the specified file in
    this dependency:

    ```python
    load("@static-checks//go.star", "go")

    register_check(go.gosec)
    ```

    5. and 6. Loading a specific file in external package, using a fully
    qualified URI instead of an alias. The delimitation is still "//" between
    the resource and the path within the package:

    ```python
    # Implicitly loads api.star
    load("@go.fuchsia.dev/shac-project/static-checks", "gosec")
    # or
    load("@go.fuchsia.dev/shac-project/static-checks//go.star", "gosec")

    register_check(gosec)
    ```

  Args:
    module: Path to a module to load. Three forms are accepted. The default is
      a relative path relative to the current module path. Second, it can be
      relative to the root of the project by using the "//" prefix. Third it can
      be an external package by using the "@" prefix. External references can
      either be fully qualified or an alias. Either way, it has to be specified
      in shac.textproto. An optional part, delimited by "//" can be appended,
      which specifies a file within the package. If omitted, the file api.star
      is loaded. When loading an external dependency and a path is specified,
      the path cannot contain ".." or "internal". Unlike bazel, the ":<target>"
      form is not allowed.
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
    *args: Arguments to print out.
    sep: (optional) Separator between the items in args. Defaults to " ".
  """
  pass


## Methods inside the shac object.


def _shac_check(impl, name = None):
  """Constructs a shac check object.

  Example:
    ```python
    def cb(ctx):
      fail("implement me")


    fail_often = shac.check(cb, name="fail_often")

    shac.register_check(fail_often)
    ```

  Args:
    impl: Starlark function that is called back to implement the check. The
      callback must accept one ctx(...) argument and return None.
    name: (optional) Name of the check. Defaults to the callback function name.
  """
  pass


def _shac_register_check(check):
  """Registers a shac check.

  It must be called at least once for the starlark file to be a valid check
  file. Each callback will be run in parallel. Each check must have a different
  name.

  Example:
    ```python
    def cb(ctx):
      fail("implement me")

    fail_often = shac.check(cb, name="fail_often")

    shac.register_check(fail_often)
    ```

    register_check also accepts a bare function for convenience when registering
    simple checks. The callback function name will be used as the check name.

    ```python
    def fail_often(ctx):
      fail("implement me")

    shac.register_check(cb, fail_often)
    ```

  Args:
    check: `shac.check()` object or Starlark function that is called back to
      implement the check.
  """
  pass


# shac is the global available at runtime when loading your starlark code.
shac = struct(
  check = _shac_check,
  # The git hash of shac.git where shac was built.
  commit_hash = "<hash>",
  register_check = _shac_register_check,
  # The semver version number of shac.
  version = (0, 0, 1),
)


def struct_(**kwargs):
  """Creates and return a structure instance.

  Create an "object" that has immutable properties. It is similar to a
  dictionary is usage. It is intentionally not as powerful as a python class
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
    **kwargs: structure's fields. The argument name becomes the property name,
      and the argument value becomes the property value.
  """
  pass
