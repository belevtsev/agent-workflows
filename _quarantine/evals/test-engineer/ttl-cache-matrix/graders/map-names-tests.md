---
type: llm
focus: { source: file, path: artefacts/requirement-map.md }
---

The contract's requirements, in short:

- R1: `New` rejects an invalid capacity, an invalid TTL and a nil clock.
- R2: `Set` stores the value with expiry `clock() + ttl`, replaces an existing
  entry and marks the key most recently used.
- R3: `Get` returns a live entry and marks it most recently used; a missing
  or expired entry returns `(zero, false)`, and an expired one is removed.
- R4: adding a key to a full cache evicts expired entries first, then the
  least recently used live entry.
- R5: `Delete` removes the entry and reports whether a live one was present.
- R6: `Len` counts live entries and never exceeds the capacity.
- R7: an entry is expired when `clock()` is at or after its expiry, and time
  comes only from `clock`.
- R8: all methods are safe for concurrent use.

PASS if each of R1 to R8 is on its own line that names at least one test by
file and test function name, or says `uncovered` with a concrete reason, and
on each line at least one named test is, by its name or the line's note, a
test of that requirement. A line may name further tests or add a note.
FAIL if a requirement is missing; a line names no test function and is not
`uncovered` with a reason; the map gives no file for a named test; or no test
named on a line fits that line's requirement, judged by the test names and
any note (for example a test of `New` alone on the line of a requirement
about expiry).
