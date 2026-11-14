---
type: llm
focus: { source: file, path: artefacts/requirement-map.md }
---

The contract's requirements, in short:

- R1: `NewBudget` rejects an invalid ratio, a negative minimum and a nil
  clock.
- R2: `RecordRequest` counts one first attempt at `clock()`.
- R3: `Allow` returns true while the retries in the window are fewer than
  `max(minRetries, ceil(ratio * requests))`.
- R4: a true `Allow` counts one retry; a false one counts nothing.
- R5: the window slides over 10 seconds, and time comes only from `clock`.
- R6: all methods are safe for concurrent use, and concurrent `Allow` calls
  never grant more than the limit.
- R7: `Stats` returns the requests and retries in the window.

PASS if each of R1 to R7 is on its own line that names at least one test by
file and test function name, or says `uncovered` with a concrete reason, and
on each line at least one named test is, by its name or the line's note, a
test of that requirement. A line may name further tests or add a note.
FAIL if a requirement is missing; a line names no test function and is not
`uncovered` with a reason; the map gives no file for a named test; or no test
named on a line fits that line's requirement, judged by the test names and
any note (for example a test of `New` alone on the line of a requirement
about expiry).
