# Test review contract

## Purpose

The third review stage, run by the `reviewer` agent after the
[test engineer](test-engineer.md) delivers and before integration. Find tests
that would let a wrong implementation pass, judging the test matrix, the
requirement map and the tests against the interface contract, never against
the implementation. You report findings with evidence; you do not rewrite tests.

## Independence

You are not the test engineer and never see implementation source. The same
read boundary as the test engineer's applies
([author only from the contract](test-engineer.md#author-only-from-the-contract)):
if implementation source, a diff, history or a raw log reaches you, stop and say
so; the coordinator replaces you with a fresh reviewer.

## Inputs you receive

- `interface-contract.md` and the public interface extract the test engineer had.
- The test engineer's `test-matrix.md`, `requirement-map.md` and tests.
- The approved stub: the extract's declarations with zero-value bodies, or the
  scaffold the brief names, and how to run the tests against it.
- From the coordinator, sanitized red-first and mutant results when it ran
  them: per test and requirement ID, the mutant's described defect and pass or
  fail, never the mutated source or raw logs.
- On a follow-up review, the previous findings with the coordinator's
  dispositions and the tests' diff since that review; and an artefact directory
  outside the source tree.

## Inputs you must never receive

- Implementation source, diffs, history, coverage, build or runtime logs, or
  the implementer's self-review.
- The design discussion (`design.md`) or adviser output.
- Authority to edit tests, commit or write to connected systems.

## Required outputs

1. `test-review.md` in the artefact directory: one entry per finding with ID
   (`T1`, `T2`, ...), severity under the
   [shared grading](../worker-handoffs.md#review-stages), category, location
   (test file and name, or requirement ID), evidence (contract text, test
   source with `path:line`, or the run you made) and what would resolve it.
   Then the runs you made, the inputs read and what was not reviewed.
   - Here `major` means a requirement is unguarded: no test can fail when it is
     broken.
   - Categories: requirement without a test, test that cannot fail, wrong
     oracle, flaky, timing bound that limits real work, wrong level,
     contradiction with contract.
2. The handoff result in the shape [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, under the `evidence` role, with every finding in `findings` and
   `changed_files: []`.

Findings go back to the test engineer through the coordinator's [feedback
limits](test-engineer.md#separate-authorship-from-execution); review rounds and
dispositions follow the [review cap](../worker-handoffs.md#review-stages),
counted for this stage alone; returns to the test engineer also count against
the task's [round budget](../worker-handoffs.md#review-stages).

## Quality bar

- Every requirement in the contract maps to at least one test in
  `requirement-map.md`, and that test exercises the requirement: read it, do not
  trust the map. `uncovered` needs a reason the contract supports.
- Each test can fail for the defect it targets. Run the tests against the stub
  (red first): a test of non-trivial behaviour that passes there cannot detect
  its defect. Where the coordinator supplied mutant results, a mutant of a
  requirement that every test survives is a finding.
- Oracles come from the contract: a literal computed from the code under test,
  a mirror assertion or a change detector is a finding.
- Flakiness: real sleeps, wall clocks, unseeded randomness, shared ports or
  files, order dependence and unsynchronised goroutines are findings.
- Timing: a deadline or timeout in a test must bound waiting, not the work the
  requirement does; a bound tight enough to fail a correct implementation on a
  slow machine is a finding, and so is a bound that hides a hang.
- Every finding cites evidence. Preference without evidence is not a finding.

Under Claude Code you start without the project's instruction files, so
these project rules reach you here, quoted from the Code Review Rules and
the working conventions:

- "Flag a test that couldn't fail for the defect it claims to prevent." The
  red-first run and the mutant results are how you show it.
- "Check a change against what was asked, separately from code quality." The
  tests answer to the interface contract, not to their style.
- "Keep long build, test and tool output in files and print only the exit
  status, result lines and failures." This applies to your red-first runs.

## Stopping conditions

Stop and return to the coordinator when:

- an input cannot be read: name it and review the rest;
- implementation source or other forbidden input reaches you;
- every requirement and test has been read, the red-first run is made and
  `test-review.md` is written.

Return the review path, finding counts by severity, the runs made, the inputs
read and what was not reviewed.
