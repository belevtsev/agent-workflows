# Adversarial tester contract

## Purpose

An opt-in white-box stage: read the integrated source and try to break it. Every
test you keep must fail on the current implementation and cite the requirement
ID or design rule it shows is broken. A test that passes is discarded.

## Opt-in and record

At classify, before `cw task start`, the coordinator opts in when the change
touches concurrency, parsing or untrusted input, security or data integrity, or
the user asks; otherwise this stage is skipped. The plan then holds a
requirement naming the opted-in areas and a check that writes a coverage profile
outside the source tree. A fresh `adversarial-tester`, no author or reviewer
of the change, runs in parallel with
[code review](code-review.md) on the same source. It records as an `evidence`
assignment and never changes source.

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

## Outputs

1. Kept tests in the artefact directory, each a new file for a path outside every
   path the test engineer owns, naming its finding ID and cited rule; state a
   design rule in contract terms, never quoting `design.md`.
2. `adversarial-findings.md` there: per finding an ID (`A1`, ...), a
   [severity](../worker-handoffs.md#review-stages), the cited rule, the test,
   the exact command, the observed failure and the failure scenario. Then the
   discarded probes with why, and what was not probed.
3. The handoff result: every finding in `findings`, `changed_files: []`.

## After a finding

Record this handoff and its acceptance before the code review's handoff, and
both before any source change. A kept test follows the
[reproduction rule](code-review.md#reproduce-every-behaviour-finding): the
coordinator adds it unchanged at a path no implementation assignment owns, the
fix turns it green without editing it, and its finding is `resolved` only when
`check_ids` cites a passing recorded run of it, `evidence` names the test and
`reason` says the adversarial tester wrote it with source access. It stays as a
regression test, never black-box coverage or in the requirement map. A test
citing a rule the contract and design do not state is rejected and not added.
Only an adversarial tester corrects a kept test that is wrong or stops
building, against the pre-fix source; otherwise the coordinator drops it and
rejects its finding. After any later source change, and before the follow-up
review's handoff, close this assignment `unused` with those dispositions. For a
requirement the black-box suite leaves unguarded, a fresh test engineer gets
that requirement ID only.

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
the question by requirement or rule ID; the coordinator settles it under the
[contract amendment](README.md#contract-amendment) procedure.
