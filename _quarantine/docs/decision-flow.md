# How workflow decisions fork

Advisers are optional. An adviser participates only when it is configured: enabled
in the policy's `consultation.providers` with its pinned model, with its key
present, opted in to (`advisers.enabled`, a key saved with `cw keys set`, or
`--policy`; a key in the environment alone is not enough), and not disabled in
your preferences (`advisers.disabled` in
`preferences.json` in the configuration directory, edited by hand; this applies to
tasks started afterwards). The shipped policy enables SubQ Snap `subq-snap-preview` and OpenAI
Decisions `gpt-6-luna`; TypeSafe Jev is opt-in with an exact version such as
`jev-1.14.0`. `cw task start` binds the configured set, and both checkpoints use
that binding. Checkpoints run when the task has an open decision: real
alternatives with consequential uncertainty. When the approach follows directly
from the defect or contract and no changed code must behave alike on several platforms or shells,
the plan declares
`"checkpoints": {"required": false, "reason": "..."}`, the flow goes from
classification to implementation and from review to `cw task verify`, and verify
lists the reason as a limitation. When in doubt, keep them required. If a task
started with checkpoints not required later meets an open decision or a change to such
cross-platform or cross-shell code, the coordinator stops and asks the user; a task started with them required does
not stop. Configured advisers receive the same permitted evidence and
questions. The coordinator checks their judgments against sources and verification
and makes the final decision; agreement, confidence and voting do not establish
truth.

## Task flow and rework

This diagram covers [substantial tasks](../global/AGENTS.md#substantial-tasks),
as the working conventions define them. Every other task follows the rule in the
same section: the agent writes the tests itself, checks its diff against the Code
Review Rules and commits or pushes only with the user's authorization.
No required state is skipped, and an unresolved decision blocks the next state.

<img src="diagrams/decision-flow.svg" alt="Decision flow of a substantial task: classify, then the before checkpoint when an open decision requires it, then the authorized work (implement, or plan, draft or review only), check and independent review, the final checkpoint when required, and cw task verify; an unresolved checkpoint leads to investigating the gap and consulting again, and a needed revision leads to revise and recheck" width="820">

When required, the before checkpoint precedes implementation, or substantive
drafting or review for a task without implementation, and `cw task assign` is
refused until it is attached and resolved. It cannot substitute for the final checkpoint. Rework
reruns every check the records cite after a source change, and refreshes affected
consultations when the evidence, revision or question meaning changes. Reuse remains within the same applicable checkpoint. Adviser
judgments grant no implementation or publication permission.

## Decision forks inside each checkpoint

For each question, a participating adviser without its key is recorded as
`not_configured` and gets no request; every other one gets one attempt, and its
valid answers are kept. The comparison status below follows from the choices
observed, and in every case the coordinator then inspects sources and relevant
checks: with enough verified evidence the question resolves with typed entries,
otherwise it stays unresolved with a null choice and a reason naming the gap.
Provider status, record status and the coordinator's final decision are
recorded separately.

| Observed choices for one question | Comparison status | Next step |
| --- | --- | --- |
| No adviser attempted (none configured) | `no_advisers` | The coordinator decides on source evidence and checks under the same rules |
| Every attempted adviser chose the same option | `agreement` | Verify the claim against evidence; on untrusted text, set `agreement_not_independent` |
| Every attempted adviser chose; choices differ | `disagreement` | Investigate; give a reason for each rejected judgment and a note for each dissent with confidence of 0.7 or higher |
| Some choices, at least one adviser missing | `partial` | Keep those choices and the missing-coverage map; verify independently. If the recorded choices differ, give the same rejection reasons and confident-dissent notes as for `disagreement` |
| No choices | `unavailable` | Record the gap and continue source-based work where possible |

`not_configured` means a participating adviser had no key, and no request was made.
`unavailable` is only for an attempted adviser that failed. A refusal supplies no
choice and does not contradict the claim; OpenAI can refuse one question while
answering others. None of these automatically rejects a claim or blocks all useful
work, but a bound adviser that is `not_configured` makes the checkpoint incomplete
until it is consulted with its key.

Each batch runs concurrently with a shared 30-second budget, one attempt per
adviser and no automatic retries or replacement model. With no configured adviser,
`cw consult` makes no network call and writes a `no_advisers_configured` record. A
material evidence change calls for an affected consultation to be refreshed,
rather than repeatedly asking unchanged questions until models agree.

`resolved` means a question has an evidenced substantive choice, which may support
or contradict a claim. It does not mean approval. Evidence and verification must be
typed `source:`, `check:` or `observation:` entries; adviser output is never
evidence. Check IDs, run IDs and observation text cannot name an adviser,
including its provider ID, name or the pinned model `gpt-6-luna`; `source:` paths
are exempt because they cite repository files. Matching is on whole words, and any
character other than a letter or digit, including `_`, separates words:
`check:jev_answer` and `check:subq_snap_choice` are rejected, while
`check:go-test-snapshot` is accepted. Name planned checks without adviser words,
such as `snap-test`, `advisor-lint` or `jev_smoke`, or they cannot be cited. Missing material facts remain unresolved even if all advisers agree; the
final record resolves only when every question resolves.

Source contracts: [task orchestration](../skills/task-orchestration/SKILL.md),
[consultation helper](../skills/task-orchestration/references/consultation-helper.md),
[model policy](../skills/task-orchestration/model-policy.yaml),
[collection and adjudication](https://github.com/belevtsev/agent-workflows/blob/main/internal/consult/record.go),
[OpenAI response handling](https://github.com/belevtsev/agent-workflows/blob/main/internal/consult/provider.go),
and [global working scope](../global/AGENTS.md).
