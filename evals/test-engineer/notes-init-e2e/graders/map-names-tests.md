---
type: llm
focus: { source: file, path: artefacts/requirement-map.md }
---

The contract's requirements, in short:

- R1: `notes init` creates `PATH/.notes/config.json` and the `.notes` directory
  with exact content and modes; `PATH` defaults to the working directory.
- R2: stdout is one JSON object with `--json`, else the single line
  `initialized <path>`.
- R3: a repeated `init` changes no file and reports `"created": false`.
- R4: a `.notes` that is a symbolic link or a regular file gives exit 2, writes
  nothing and prints an error.
- R5: a missing `PATH` gives exit 2 and is never created.
- R6: nothing is written outside `PATH/.notes` or read outside `PATH`.
- R7: no escape sequences with `TERM=dumb` or a non-terminal stdout, and the
  command never waits for input.

PASS if each of R1 to R7 is on its own line that names at least one test by
file and test function name, or says `uncovered` with a concrete reason, and
on each line at least one named test is, by its name or the line's note, a
test of that requirement. A line may name further tests or add a note.
FAIL if a requirement is missing; a line names no test function and is not
`uncovered` with a reason; the map gives no file for a named test; or no test
named on a line fits that line's requirement, judged by the test names and
any note (for example a test of `New` alone on the line of a requirement
about expiry).
