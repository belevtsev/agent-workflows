---
type: llm
focus: { source: file, path: artefacts/adversarial-findings.md }
---

The integrated source has one seeded defect. `Decode` reads the uvarint
length prefix `n` (a `uint64`) and passes it to the unexported helper
`checkLength(n, max)`, which converts `n` to `int` first (`size := int(n)`)
and only then compares `size > max`. For any `n` of 2^63 or more, up to
2^64-1, the conversion wraps to a negative `int`, the check passes, and
`make([]byte, size)` panics with `runtime error: makeslice: len out of range`.
This violates R2 (every `n > max` up to and including 2^64-1 returns
`ErrTooLarge` and a nil slice before reading or allocating the payload, and
`Decode` never panics on any prefix) and DR1 (compare the length to `max` as a
`uint64` before any conversion to `int`). For `n` above `max` but below 2^63
the seeded source correctly returns `ErrTooLarge`; the black-box tests pass
because they only use lengths near `max`. A probe is a 10-byte prefix such as
`binary.AppendUvarint(nil, 1<<63)` (`80 80 80 80 80 80 80 80 80 01`) or
2^64-1 (`ff` nine times then `01`) with a small `max`, expecting
`ErrTooLarge`; the seeded source panics instead.

PASS if the findings file has a finding with an ID (`A1`, ...) that describes
this failure scenario (a length prefix of 2^63 or more, far above `max`, is
not rejected and makes `Decode` panic or allocate with a wrapped negative
length); cites R2 or DR1 by ID; is graded `blocker` or `major`; gives the
exact `go test` command that names the test (a `-run` with the test name);
and quotes the observed failure from a real run (for example the
`makeslice: len out of range` panic, or a negative length).
FAIL if no finding describes this defect, the finding cites no requirement or
design-rule ID, it is graded `minor` or lower, it gives no command or no
observed failure, it presents the defect only as theory without a failing
run, or its expected value is taken from the current output (for example
treating the panic as expected) rather than from R2 or DR1.
