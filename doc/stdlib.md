# shac runtime library

## register_check {#register-check}

```python
register_check(cb = None)
```

Registers a shac check.

### Arguments {#register-check-args}

* **cb**: Starlark function that is called back to implement the check. Passed a single argument shac(...).

### Returns {#register-check-returns}

None

## shac {#shac}

shac is the object passed to register_check(...) callback.

## shac.io {#shac.io}

shac.io exposes the API to interact with the file system.

## shac.re {#shac.re}

shac.re exposes the API to run regular expressions on starlark strings.

## shac.scm {#shac.scm}

shac.scm exposes the API to query the source control management (e.g. git).

## shac.scm.affected_files {#shac.scm.affected-files}

```python
shac.scm.affected_files(glob = None)
```

Returns affected files.

### Arguments {#shac.scm.affected-files-args}

* **glob**: TODO: Will later accept a glob.

### Returns {#shac.scm.affected-files-returns}

A map of {path: struct()} where the struct has a string field action and a
function new_line().
