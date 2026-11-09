# Interface contract: package `jail`

Lexical confinement of untrusted relative names to a root directory, so a
server can map request paths to files without leaving its data directory.
Language: Go. Module `example.com/jail`, package `jail` at the module root.
Unix-style paths only.

## Public interface

```go
var ErrOutside error // the name resolves outside the root

type Root struct{ /* unexported fields */ }

func New(dir string) (*Root, error)
func (r *Root) Resolve(name string) (string, error)
```

## Requirements

- R1: `New(dir)` with an absolute `dir` whose cleaned form is not `/`
  returns a `Root` for the cleaned `dir`. A relative `dir`, or one that cleans
  to `/`, returns a nil `Root` and a non-nil error.
- R2: `Resolve(name)` with a relative `name` whose result lies inside the
  root returns `filepath.Join(root, name)` (the cleaned absolute path) and a
  nil error.
- R3: `Resolve(".")` and `Resolve("")` return the cleaned root and a nil
  error.
- R4: An absolute `name` returns `""` and `ErrOutside` (`errors.Is` matches
  it), even when it names a path inside the root.
- R5: A relative `name` whose cleaned result lies outside the root returns
  `""` and `ErrOutside`. Outside means anything other than the root itself or
  a path below it in the directory tree, for example any `..` escape.
- R6: `Resolve` is lexical: it never touches the filesystem, so symlinks are
  out of scope. A `Root` is immutable and safe for concurrent use.

## Scope

- No symlink resolution, no Windows paths, no opening of files.
