# How workflow decisions fork

Advisers are optional. An adviser participates only when it is configured: enabled
in the policy's `consultation.providers` with its pinned model, with its key
present, and not disabled in your preferences (setup's Keys step, or
`advisers.disabled` in a `--settings` file; this applies to tasks started
afterwards). The shipped policy enables SubQ Snap `subq-snap-preview` and OpenAI
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

This diagram covers substantial tasks: independent workstreams, cross-component
changes (including code that must behave alike on several platforms or shells) or
consequential uncertainty, and any task when unsure. Small edits and direct factual answers
retain the lightweight path, including relevant verification and user authorization.
No required state is skipped, and an unresolved decision blocks the next state.

```mermaid
flowchart TD
    Classify[Classify the task<br/>cw task start binds configured advisers]
    Open{Open decision, code that must behave<br/>alike across platforms or shells, or in doubt?}
    Before[Before checkpoint<br/>consult configured advisers, adjudicate, attach]
    BeforeOK{Before adjudication}
    Mode{What work did the user authorize?}
    Build[Implement within the authorized scope<br/>cw task assign allowed from here]
    Draft[Plan, draft or review only]
    Check[Integrate, run the selected checks, independent review]
    Final[Fresh final checkpoint<br/>completed work and current checks]
    FinalOK{Final adjudication}
    Ready[cw task verify, then report<br/>Publish only when already authorized]
    Rework[Revise within scope and recheck<br/>Refresh affected evidence and advice]
    Gap[Investigate the material gap<br/>Adjudicate again or consult with new evidence]

    Classify --> Open
    Open -->|Yes| Before --> BeforeOK
    Open -->|No: checkpoints not required| Mode
    BeforeOK -->|Unresolved| Gap
    BeforeOK -->|Resolved| Mode
    Mode -->|Changes authorized| Build
    Mode -->|Plan or review only| Draft
    Build --> Check
    Draft --> Check
    Check -->|Checkpoints required| Final --> FinalOK
    Check -->|Not required| Ready
    FinalOK -->|Resolved| Ready
    FinalOK -->|Unresolved| Gap
    FinalOK -->|Needs revision| Rework
    Rework --> Check
    Gap -->|Before| Before
    Gap -->|Final| Final
```

When required, the before checkpoint precedes implementation, or substantive
drafting or review for a task without implementation, and `cw task assign` is
refused until it is attached and resolved. It cannot substitute for the final checkpoint. Rework
reruns every check the records cite after a source change, and refreshes affected
consultations when the evidence, revision or question meaning changes. Reuse remains within the same applicable checkpoint. Adviser
judgments grant no implementation or publication permission.

## Decision forks inside each checkpoint

The branches below describe each question. Provider status, record status and the
coordinator's final decision are recorded separately.

```mermaid
flowchart TD
    Input[Same permitted evidence, instructions and Choice options]
    Set{Participating adviser has its key?}
    Skip[not_configured<br/>no request]
    Attempt[One attempt per configured adviser<br/>30-second batch budget]
    Validate[Validate results independently<br/>Keep valid answers and explicit missing coverage]
    Coverage{Choice coverage for this question?}
    Match{Do all attempted advisers' choices match?}
    NoAdv[no_advisers<br/>nothing attempted]
    Agree[agreement]
    Differ[disagreement]
    Partial[partial<br/>some choices, some missing]
    None[unavailable<br/>no choices]
    Evidence[The coordinator inspects sources and relevant checks<br/>Investigate disputed or missing facts]
    Facts{Enough verified evidence to decide?}
    Resolved[Resolved: typed source, check or observation entries<br/>rejection reasons, investigation notes]
    Unresolved[Unresolved: choice null<br/>reason names the material gap]

    Input --> Set
    Set -->|No| Skip
    Set -->|Yes| Attempt
    Attempt --> Validate
    Skip --> Validate
    Validate --> Coverage
    Coverage -->|Nothing attempted| NoAdv
    Coverage -->|Every attempted adviser chose| Match
    Match -->|Yes| Agree
    Match -->|No| Differ
    Coverage -->|Some choices| Partial
    Coverage -->|No choices| None
    NoAdv --> Evidence
    Agree --> Evidence
    Differ --> Evidence
    Partial --> Evidence
    None --> Evidence
    Evidence --> Facts
    Facts -->|Yes| Resolved
    Facts -->|No| Unresolved
```

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
