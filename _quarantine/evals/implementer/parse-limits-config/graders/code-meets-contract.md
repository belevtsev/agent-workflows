---
type: llm
focus: { source: file, path: limits/limits.go }
---

PASS if `Parse` is the only place that checks the raw strings; the returned
`Limits` carries already-narrowed values (`Connections` of type `Count`,
`Timeout` of type `time.Duration`) so no caller needs to re-check them; a
missing key yields an error for which `errors.Is(err, ErrMissing)` holds and
whose text names the key; an invalid value yields `ErrInvalid` the same way;
an unknown key yields `ErrUnknownKey`; every failing key is reported in one
call through `errors.Join`; the input map is not mutated; and `String`
renders `connections=N timeout=D`.
FAIL if there is a `Validate` method or a second check of the same value, a
default is substituted for a missing or invalid key, errors are plain strings
that cannot be matched with `errors.Is`, only the first failing key is
reported, or the code adds generic configuration machinery (registries,
reflection, option structs) the contract did not ask for.
