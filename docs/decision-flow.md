# How workflow decisions fork

Jev `jev-1.14.0`, SubQ `subq-snap-preview` and OpenAI Decisions `gpt-6-luna`
are required advisers at equal standing. They receive the same permitted evidence
and questions. The coordinator checks their judgments against sources and verification and
makes the final decision; agreement, confidence and voting do not establish truth.

## Task checkpoints and rework

This diagram covers substantial tasks. Small edits and direct factual answers
retain the lightweight path, including relevant verification and user authorization.
The coordinator owns checkpoint timing; the helper does not schedule these phases.

```mermaid
flowchart TD
    Scope[Establish scope, authorization and source evidence]
    First[Checkpoint 1: Jev + Snap + OpenAI]
    Mode{What work did the user authorize?}
    Build[Implement within the authorized scope]
    Draft[Plan, draft or review only]
    Check[Integrate and run relevant verification]
    Final[Checkpoint 2: fresh advice from all three<br/>Completed work and current checks]
    Decide{The coordinator independently assesses the evidence}
    Ready[Report the verified result<br/>Publish only when already authorized]
    Rework[Revise within scope and recheck<br/>Refresh affected evidence and advice]
    Unknown[Record unresolved material facts<br/>Continue useful work; request input only if needed]

    Scope --> First --> Mode
    Mode -->|Changes authorized| Build
    Mode -->|Plan or review only| Draft
    Build --> Check
    Draft --> Check
    Check --> Final --> Decide
    Decide -->|Verified conclusion| Ready
    Decide -->|Needs revision| Rework
    Rework --> Mode
    Decide -->|Material unknown| Unknown
```

The first checkpoint precedes implementation, or substantive drafting/review for a
task without implementation. It cannot substitute for the final checkpoint.
Rework refreshes affected checks and consultations when the evidence, revision or
question meaning changes. Reuse remains within the same applicable checkpoint.
Adviser judgments grant no implementation or publication permission.

## Decision forks inside each checkpoint

The branches below describe each question. Provider status, overall consultation
availability and the coordinator's final decision are recorded separately.

```mermaid
flowchart TD
    Input[Same permitted evidence, instructions and Choice options]
    Jev[Jev attempt<br/>jev-1.14.0]
    Snap[SubQ Snap attempt<br/>subq-snap-preview]
    OpenAI[OpenAI Decisions attempt<br/>gpt-6-luna]
    Validate[Validate results independently<br/>Keep valid answers and explicit missing coverage]
    Coverage{Choice coverage for this question?}
    Match{Do all three choices match?}
    Agree[Agreement]
    Differ[Disagreement]
    Partial[Partial coverage<br/>Some choices; at least one missing]
    None[Unavailable coverage<br/>No choices]
    Evidence[The coordinator inspects sources and relevant checks<br/>Investigate disputed or missing facts]
    Facts{Enough verified evidence to decide?}
    Resolved[Resolved question with a substantive choice<br/>Reason, evidence, checks and rejected-judgment reasons]
    Unresolved[Unresolved question<br/>choice: null; explain the material gap]

    Input -->|Key present| Jev
    Input -->|Key present| Snap
    Input -->|Key present| OpenAI
    Input -.->|Per-provider missing key: unavailable without HTTP| Validate
    Jev --> Validate
    Snap --> Validate
    OpenAI --> Validate
    Validate --> Coverage
    Coverage -->|All three choices| Match
    Match -->|Yes| Agree
    Match -->|No| Differ
    Coverage -->|Some choices| Partial
    Coverage -->|No choices| None
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
| All three choices match | `agreement` | The coordinator verifies the claim against evidence |
| All three answer; choices differ | `disagreement` | The coordinator investigates and explains each rejected judgment |
| Some choices, including two conflicting answers and one missing adviser | `partial` | Keep those choices and the missing-coverage map; the coordinator verifies independently |
| No choices | `unavailable` | Record the gap and continue source-based work where possible |

A refusal supplies no choice and does not contradict the claim. OpenAI can refuse
one question while returning valid choices for others. Missing credentials,
timeouts, malformed responses and service errors remain explicit coverage limits;
they do not automatically reject a claim or block all useful work.

Each batch runs concurrently with a shared 30-second budget, one attempt per
provider and no automatic retries or replacement model. Required advisers cannot
be silently disabled. A material evidence change calls for an affected consultation
to be refreshed, rather than repeatedly asking unchanged questions until models agree.

`resolved` means a question has an evidenced substantive choice, which may support
or contradict a claim. It does not mean approval. The coordinator records a reason for every
actual provider choice it rejects. Missing material facts remain unresolved even
if all three advisers agree; the final record resolves only when every question
resolves.

Source contracts: [task orchestration](../skills/task-orchestration/SKILL.md),
[model policy](../skills/task-orchestration/model-policy.yaml),
[collection and adjudication](https://github.com/belevtsev/agent-workflows/blob/main/internal/consult/record.go),
[OpenAI response handling](https://github.com/belevtsev/agent-workflows/blob/main/internal/consult/provider.go),
and [global working scope](../global/AGENTS.md).
