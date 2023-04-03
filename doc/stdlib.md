# shac runtime standard library

The starlark language specification is documented at
https://github.com/google/starlark-go/blob/HEAD/doc/spec.md. It is a python
derivative.

Note: The standard library is implemented in native Go.

## register_check

```python
register_check(cb = None)
```

Registers a shac check.

### Arguments

* **cb**: Starlark function that is called back to implement the check. Passed a single argument shac(...).

## shac

shac is the object passed to register_check(...) callback.

## shac.io

shac.io is the object that exposes the API to interact with the file system.

## shac.io.read_file

```python
shac.io.read_file(path = None)
```

Returns the content of a file.

### Arguments

* **path**: path of the file to read. The file must be within the workspace. The path must be relative and in POSIX format, using / separator.

### Returns

Content of the file as bytes.

## shac.re

shac.re is the object that exposes the API to run regular expressions on
starlark strings.

## shac.re.allmatches

```python
shac.re.allmatches(pattern = None, str = None)
```

Returns all the matches of the regexp pattern onto content.

### Arguments

* **pattern**: regexp to run. It must use the syntax as described at https://golang.org/s/re2syntax.
* **str**: string to run the regexp on.

### Returns

list(struct(offset=bytes_offset, groups=list(matches)))

## shac.re.match

```python
shac.re.match(pattern = None, str = None)
```

Returns the first match of the regexp pattern onto content.

### Arguments

* **pattern**: regexp to run. It must use the syntax as described at https://golang.org/s/re2syntax.
* **str**: string to run the regexp on.

### Returns

struct(offset=bytes_offset, groups=list(matches))

## shac.scm

shac.scm is the object exposes the API to query the source control
management (e.g. git).

## shac.scm.affected_files

```python
shac.scm.affected_files(glob = None)
```

Returns affected files as determined by the SCM.

If shac detected that the tree is managed by a source control management
system, e.g. git, it will detect the upstream branch and return only the files
currently modified.

If the current directory is not controlled by a SCM, the result is equivalent
to shac.scm.all_files().

If shac is run with the --all options, all files are considered "added" to do
a full run on all files.

### Arguments

* **glob**: TODO: Will later accept a glob.

### Returns

A map of {path: struct()} where the struct has a string field action and a
function new_line().

## shac.scm.all_files

```python
shac.scm.all_files(glob = None)
```

Returns all files found in the current workspace.

It considers all files "added".

### Arguments

* **glob**: TODO: Will later accept a glob.

### Returns

A map of {path: struct()} where the struct has a string field action and a
function new_line().

## shac.exec

```python
shac.exec(cmd = None, cwd = None)
```

Runs a command as a subprocess.

### Arguments

* **cmd**: Subprocess command line.
* **cwd**: Relative path to cwd for the subprocess.

### Returns

An integer corresponding to the subprocess exit code.
