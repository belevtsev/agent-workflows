# Build a feedback loop first

Use this for a hard bug, before forming theories. A tight pass/fail signal that goes red on this exact bug is most of the work, because bisection, hypotheses, and instrumentation all depend on it. Without one, reading code just produces plausible stories.

## Get one command that goes red

Before theorizing, have one command you've already run that:

- drives the real code path and asserts the reported symptom, rather than merely not crashing,
- gives the same verdict each run,
- finishes in seconds,
- runs unattended.

Try these in roughly this order: a failing test at the seam that reaches the bug, a `curl` against a local server, a CLI call on a fixture compared with known-good output, replaying a captured request, a throwaway harness, a property or fuzz loop, `git bisect run`, or running old and new versions on one input and comparing them.

Then tighten it. Narrow the scope so it's faster, assert the specific symptom, and pin time, randomness, and the filesystem. A 30-second flaky loop is barely better than none.

For a flaky bug, the goal is a higher reproduction rate, not a clean reproduction. Use `go test -count=1000`, `-race`, and `-cpu` variations, and add stress or narrow timing windows. A failure at 50% can be debugged, and one at 1% can't.

If you genuinely can't build a loop, stop and say so. List what you tried, and ask for access to the environment, a captured artifact, or permission for temporary instrumentation.

## Minimize

Once it's red, confirm it's the failure that was reported, not a different one nearby. Then cut inputs, callers, config, and data one at a time, rerunning after each. You're done when every remaining element is load-bearing, meaning removing any one makes it pass. Fewer moving parts leaves fewer suspects, and the result becomes a clean regression test.

## Rank hypotheses before testing

Write three to five hypotheses before testing any. A single hypothesis anchors you on the first plausible idea. Each must make a prediction: "If X is the cause, changing Y makes the bug disappear." If you can't state one, sharpen the hypothesis or drop it.

Share the ranked list, since someone may already know that the third one changed yesterday. Then change one variable at a time, and map each probe to a prediction. Tag temporary logs with a unique prefix, so cleanup is a single search.

## Fix and lock it down

Write the regression test before the fix, but only at a seam that exercises the real bug. A test at too shallow a seam gives false confidence. If no correct seam exists, that's a finding in itself, so report it. If you forced a failure by changing code or a fixture, confirm the change actually landed before trusting the result.

After the fix, rerun the original unminimized scenario, remove temporary instrumentation, and record which hypothesis was right in the commit message.

_Adapted from `diagnosing-bugs` in `mattpocock/skills`, and rewritten for Go._
