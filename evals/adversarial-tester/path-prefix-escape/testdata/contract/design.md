# Design: package `jail`

## Shape

`Root` stores the cleaned root directory. `Resolve` rejects absolute names,
joins the name to the root with `filepath.Join` (which cleans the result) and
passes the joined path to a containment helper.

## Design rules

- DR1: Containment means the joined, cleaned path equals the root or starts
  with the root followed by a path separator (`root + "/"`). A path that
  merely starts with the root's string, without a separator after it, is not
  contained.
- DR2: Containment is checked on the cleaned joined path only, after
  `filepath.Join`, never on the raw name.
- DR3: `Resolve` uses only `path/filepath` and `strings`; it makes no system
  call (R6).
