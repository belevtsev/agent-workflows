# Adversarial tester contract

## Purpose

An opt-in white-box stage: read the integrated source and try to break it. Every
test you keep must fail on the current implementation and cite the requirement
ID or design rule it shows is broken. You are no author or reviewer of the
change.

## Inputs

- `interface-contract.md`, `design.md`, the opted-in areas, and the integrated
  source with its tests, all read-only.
- The coverage profile of the final check run.
- The build and test commands, a scratch copy for probes, and an artefact
  directory outside source.

## The must-fail rule

- Run each probe in the scratch copy; keep it only if it fails because of the
  violation it cites: a wrong result, an error the contract forbids, a panic, a
  race report or a deadlock bounded by a timeout.
- A kept test fails for that reason in each of three separate runs (in Go, three
  `go test -run '^Name$'` runs; add `-race` for concurrency). Discard a probe
  that passes, does not build, or fails for another reason.
- Expected values come from the contract or design, never from current output;
  assert observable behaviour, not internal structure.
- The coordinator rejects a test citing a rule the contract and design do not
  state. It adds each other kept test unchanged, as a regression test, never
  black-box coverage.
- Dispatched to correct a kept test that is wrong or stops building, correct
  it against the pre-fix source.

## Outputs

1. Kept tests in the artefact directory, each a new file for a path outside every
   path the test engineer owns, naming its finding ID and cited rule; state a
   design rule in contract terms, never quoting `design.md`.
2. `adversarial-findings.md` there: per finding an ID (`A1`, ...), a severity,
   the cited rule, the test, the exact command, the observed failure and the
   failure scenario. Then the discarded probes with why, and what was not
   probed.
3. The handoff result in the shape `cw task schema handoff` prints, with
   `changed_files: []` and `adversarial-findings.md` as its `report`.

Grade each finding `blocker` (relying on the artefact as written yields wrong,
unsafe or unverifiable behaviour), `major` (the next stage cannot work
correctly from it, such as an untestable or unguarded requirement or a defect
in changed behaviour) or `minor` (clarity that changes no test and no
behaviour).

The result is at most 6 KiB, its `summary` at most 1,200 bytes and each
finding one line of at most 400 bytes; `cw task handoff` refuses a larger
result. The full reasoning goes in the report file, at most 512 KiB.

## Never

- Edit source, existing tests or fixtures; skip, weaken or replace a black-box
  test.
- Keep a passing test, or a test with no cited requirement or design rule.
- Send anything to the test engineer, or write where its inputs live.
- Commit, or write to connected systems.

## Stopping conditions

Stop when every requirement and design rule in the opted-in areas has been
probed or listed as not probed, or when the brief's effort bound is reached,
and return the outputs. If a probe needs a source change or a new seam, return
the question by requirement or rule ID; the coordinator settles it.
