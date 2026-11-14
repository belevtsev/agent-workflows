---
type: llm
focus: { source: file, path: artefacts/adversarial_test.go }
---

This file holds the adversarial tester's kept tests for Go package `frame`.
The role contract says every kept test must fail on the current (seeded)
source because of the violation it cites, and must name its finding ID and
cited rule; a test that passes is discarded, not kept.

How the seeded source really behaves (on a 64-bit platform). Only one thing
is wrong: the helper `checkLength` converts the decoded length to `int`
before comparing it with `max`, so any length prefix of 2^63 or more wraps to
a negative size, passes the check and makes `make([]byte, size)` panic with
`makeslice: len out of range`, instead of returning `ErrTooLarge` (R2, DR1).
Everything else already matches the contract, so a test of it passes on the
seeded source: a length up to `max` returns exactly the payload; length 0
returns an empty non-nil slice; no byte past the frame is consumed; every
length above `max` and below 2^63 (for example `max+1`, 2^32, 2^62 or
2^63-1) returns a nil slice and `ErrTooLarge` without reading the payload; a
short payload returns `io.ErrUnexpectedEOF`; an 11-byte prefix or a 10-byte
prefix whose last byte exceeds 1 returns `ErrMalformed`; empty input returns
`io.EOF`; a prefix cut off midway returns `io.ErrUnexpectedEOF`; and an error
from the reader is returned so that `errors.Is` matches it.

PASS if the file declares `package frame`; every test function in it
(`func TestXxx(t *testing.T)`) decodes at least one length prefix of 2^63 or
more and asserts, from R2 or DR1, `ErrTooLarge` (or no panic, or no
allocation), so it fails on the seeded source for that reason; and each test
names its finding ID (`A1`, ...) and the rule it cites (R2 or DR1) in its name
or a comment.
FAIL if the file is missing or holds no test function; any test function uses
only lengths below 2^63 or otherwise asserts only behaviour listed above as
already present (it would pass on the seeded source); any test expects the
panic as correct behaviour; any test lacks a finding ID or a cited requirement
or design rule, or cites a rule the contract and design do not state; or any
test takes its expected values from the current output rather than from the
contract or design.
