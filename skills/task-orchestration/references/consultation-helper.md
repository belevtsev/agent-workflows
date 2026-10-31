# Decision consultation helpers

Read this before using `cw consult`.
It validates a prepared Choice-only request, sends the same shared input to Jev,
SubQ Snap, and OpenAI Decisions concurrently, and records each result independently. All three
are required at equal standing. It does not dispatch
workers or decide which judgment is correct. The command is part of the native `cw`
manager and needs no interpreter or package installation.

## Required task checkpoints

For every substantial task using task-orchestration, the coordinator runs this three-adviser
helper before implementation (or substantive drafting/review) and again during final
review, following the [entrypoint](../SKILL.md). Prepare distinct requests and new
output paths for the two checkpoints. The first request assesses the proposed approach
and verification plan; the final request assesses the completed work and current checks
before completion or authorized publication. Include the checkpoint in request state
and retain its provider coverage and the coordinator disposition in the task record.

Reuse is limited to the same checkpoint with applicable evidence, decisions, question
meaning, and models. A pre-implementation record cannot satisfy final review. If calls
are unavailable or sharing is restricted, record that checkpoint's limitation and
follow the source-evidence fallback; do not bypass restrictions or add retries.
The helper validates individual requests and records; the coordinator owns checkpoint
timing. Installation, update, validation, and CI do not call hosted models.

## Prepare and run a shared request

The input is JSON with exactly `state` and `questions`. Include faithful text excerpts,
the active source revision and locations, relevant constraints, and distinguishable
assumptions in `state`. Each question must be a Choice with named options. Include
`insufficient_evidence` when a material missing fact could prevent an answer. Participating
providers receive the same state, instructions, and criteria; the helper supplies
their policy-selected model IDs. This common workflow has no images or other
primitives and must fit the participating providers' documented limits.

The coordinator verifies excerpts against their exact sources before sharing them.
The helper can validate structure and compare returned choices; it cannot establish
source truth or discover omitted evidence. Ask for a bounded judgment about the
prepared evidence rather than code, findings, or explanations.

Preview the request, then send it:

```sh
cw consult --request /path/to/request.json --output /path/to/preview.json --dry-run
cw consult --request /path/to/request.json --output /path/to/new-record.json
```

`--policy /path/to/model-policy.yaml` selects a validated policy instead of the
installed release's. Without it, `cw consult` reads the policy and its questions from
`current/skills/task-orchestration` in the manager's state directory. `--dry-run`
validates the policy and input and writes a task record without network access.
A real call uses separate `TYPESAFE_API_KEY` and `SUBQ_API_KEY` and `OPENAI_API_KEY`
environment variables, and one attempt per required provider. A 30-second batch
budget covers connecting, sending, and reading every complete response; a provider
still running at the deadline is cancelled and recorded as a timeout, and it cannot
extend the batch. It neither retries nor substitutes another model.
Missing credentials, malformed responses, timeouts, and safe service failure
categories are recorded independently, so one provider's failure preserves the
other's result. Responses are validated for IDs, types, options, finite values,
distributions, usage, and returned model.

The maintained model IDs are `jev-1.14.0`, `subq-snap-preview` and `gpt-6-luna`. Snap's documented
public preview ID is not an immutable version pin; retain the requested and returned
model and consult the current contract when reusing a result. Reuse only within the
same checkpoint while the source revision, evidence, decisions, question meaning,
and models remain applicable.

### Jev version and capability boundaries

The explicit `jev-1.14.0` ID keeps a run independent of moving aliases. As checked
on 16 October 2026, TypeSafe's [model reference](https://docs.typesafe.ai/models)
maps `jev-latest` to `jev-1.13.0` and `jev-preview` to `jev-1.14.0`; consult that
reference for subsequent changes. Retain the returned version and reject a
response that differs from the requested pin. The
[1.13 jaggedness notes](https://docs.typesafe.ai/model-jaggedness/jev-1.13) apply to
1.13 and do not establish 1.14 limitations or improvements.

The [1.14 announcement](https://typesafe.ai/blog/jev-1-14) introduces experimental
[images](https://docs.typesafe.ai/images) and
[Box](https://docs.typesafe.ai/primitives/box) answers. They require
`TypeSafe-Danger-Zone-Images: true`. Extended context requires the separate
`TypeSafe-Danger-Zone-Long-Context: true` header; it raises the standard 64k request
and 32k per-question token limits to 262k for both. This helper sends neither
header, accepts no image assets and remains Choice-only for all advisers. A model
upgrade does not implicitly enable those features or authorize different evidence
for one provider. Evaluate language-dependent judgment quality on labeled inputs;
availability and provider agreement are separate evidence.

## Required OpenAI Decisions

The maintained policy declares `openai_decisions: gpt-6-luna` under
`consultation.providers` alongside required Jev and Snap. Every request includes
all three without a participation flag. Missing `OPENAI_API_KEY` records
`unavailable` required coverage; it never disables OpenAI or reports the remaining
pair as full three-provider coverage. `--without-openai` is rejected before output
creation or network access under this policy. `--with-openai` is a redundant
explicit inclusion. Adjudication follows the saved record and uses no key or
participation switch. Historical optional policies retain their existing flag
behavior; they do not change the maintained requirements.

The fixed endpoint is `POST https://api.openai.com/v1/decisions`. The adapter maps
canonical shared state to deterministic JSON text in `input`, question IDs to
`name`, and each criterion key/description to a `choices` value/description. It
normalizes named answer arrays and probability arrays into the common record.
The request retains the shared evidence fingerprint; endpoint-specific encoding
does not let an adviser receive different facts or question meanings.

Read the current [guide](https://developers.openai.com/api/docs/guides/decisions),
[create contract](https://developers.openai.com/api/reference/resources/decisions/methods/create),
and applicable [data controls](https://developers.openai.com/api/docs/guides/your-data).
The common integration remains text and Choice only. OpenAI currently accepts at
most 200 questions; an unsupported OpenAI request is recorded without breaking
the other advisers' results. OpenAI's predicate, score, image and other input features do
not expand this shared contract. No extra SDK is required.

OpenAI can refuse an individual question while returning valid answers to others.
The record keeps those choices and marks the refused question as missing coverage.
It does not turn a refusal into a negative choice. A mixed response is `partial`;
an all-refused response is `refusal`. Malformed responses, missing credentials,
timeouts and safe service failures are recorded separately. The 30-second batch
budget and one-attempt policy cover all required providers concurrently.

Required-three policies write version-three records. All three providers must be
present and none can be disabled; a missing, failed or refusing adviser leaves
explicit partial or unavailable coverage, including each question's `missing` map.
Offline adjudication rejects disabled required providers and validates every
version's provider and field contract. Historical pair-only version-one and optional
version-two records remain readable unchanged, including their original disabled
semantics. Coverage states do not decide the claim.

## Compare and retain evidence

The record contains the shared prepared input, per-provider status and validated
answers, and per-question `agreement`, `disagreement`, `partial`, or `unavailable`.
These categories describe provider coverage and choice equality. They do not establish
truth. `final_decision` starts as `pending`; no confidence comparison, vote, or
automatic winner completes it.

The coordinator investigates any disputed evidence and independently verifies the conclusion
against exact sources and relevant tests or observations, including when the providers
agree. In the agreed task record retain the final conclusion, evidence references and
source revision, verification and its limits, rejected judgment and reason, unresolved
facts, and partial or unavailable provider coverage. If a material fact remains missing
or conflicting, retain an unresolved decision. An unavailable consultation alone does
not prevent useful source work.

## Finalize the coordinator's decision without network access

Once the coordinator has done that investigation, write a decision JSON and validate it against
the original consultation record:

```sh
cw consult --adjudicate /path/to/consultation.json --decision /path/to/codex-decision.json --output /path/to/final.json
```

`cw consult` exits `0` when the consultation is complete or the decision is resolved,
`2` for partial or unavailable coverage or an unresolved decision, and `1` when input
is invalid or the output path already exists.

`--adjudicate` and `--request` are mutually exclusive. Adjudication makes no provider
requests and uses no credentials. It writes a new record, preserving the consultation
input and provider results. The decision must contain every question ID exactly once:

```json
{
  "questions": {
    "claim_support": {
      "status": "resolved",
      "choice": "supported",
      "reason": "The source establishes the claim for the stated conditions.",
      "evidence": ["src/example.go:42 at <source revision>"],
      "verification": ["Source inspection at <source revision>; focused test <result reference>"],
      "rejected_judgments": {
        "subq_snap": "The contrasting choice overlooked the stated condition at src/example.go:42."
      }
    }
  }
}
```

This example is a shape to adapt, not evidence for a real decision. Each item has
`status`, `choice`, `reason`, `evidence`, `verification`, and `rejected_judgments`.
For `resolved`, the choice must be an allowed option; evidence and verification must
be nonempty source/revision and check references. Give a rejection reason for every
successful provider choosing a different option, using the provider IDs `jev`, `subq_snap`, and `openai_decisions`.
A valid differing choice in a partially refused response still needs a rejection
reason for that question; missing or refused judgments do not. Agreement still needs independent evidence and verification.

For `unresolved`, use `choice: null`, an empty rejection mapping, and a nonblank reason
describing the material unknown; evidence and verification references may be empty. The final
record is resolved only when every question is resolved. This structural validation
checks that the coordinator recorded its decision obligations; it cannot prove the cited
evidence or reasoning correct and never chooses a winning confidence score.

## Sharing and recording

The helper writes only to the supplied new output path. Its record excludes credentials,
authorization headers, raw errors, and unknown response fields, but retains the prepared
request verbatim. Sanitize inputs before sharing or recording them; the helper cannot
remove credentials or sensitive material embedded in evidence. Keep sensitive records
in the task's permitted storage. Saving outside the agreed task location requires its
own scope.

SubQ [retains data indefinitely by default](https://docs.subq.ai/using-the-api/data-retention/).
Organization-level zero data retention is available and applies to Decisions. Its
documented per-request `store: false` setting covers Chat Completions and Responses;
the helper does not claim that setting for Decisions. Confirm that sharing and the
organization's retention policy fit the authorized task before sending source evidence.
Credentials and authentication are documented in the
[SubQ authentication guide](https://docs.subq.ai/using-the-api/authentication/).

## Multilingual evaluation

`cwdev eval multilingual` provides a small
synthetic baseline using the current three-adviser policy. It is separate from
task consultations, skill-discovery evaluations and native manager checks. The
bundled corpus lives in `evals/multilingual` relative to the skill; `cwdev` is a
maintainer tool that reads it from a source checkout, selected with `--source`
(by default the current directory).

The twelve neutral case IDs cover durable acknowledgement, tenant isolation,
cancellation and release identity. Each family has supporting, contradictory and
missing-evidence observations. English, Ukrainian and German variants preserve
the facts, claim, instructions and option meanings. The stable choices are
`supported`, `contradicted` and `insufficient_evidence`.
The request builder uses only raw inputs. The separate oracle's per-case expected
choice and evaluator rationale never enter a provider request. Allowed option
definitions are part of each normal Choice request.

Run offline validation without network access, credential lookup or output files:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual
```

Run it from the editable checkout; do not edit installed snapshots. A hosted run
requires `--live --output-dir PATH`, all three provider credentials in the
environment, and a new private directory outside source repositories and managed
releases:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual --live --output-dir ~/.codex/workflow-audits/multilingual-baseline-01
```

Freeze the reviewed input and oracle hashes before the first hosted request. Each
language forms one batch of twelve questions, with each question referring to its
own case. The three providers receive identical shared input concurrently; language
batches run sequentially. The full evaluation makes at most nine provider requests,
uses the existing 30-second batch budget and never retries. Ordinary task planning
and final-review consultations are separate from that evaluation budget.

The private `report.json`, `summary.md` and safe per-language consultation records
retain input/oracle/policy/runner provenance, runtime identity, requested and
returned models, measured latency and token usage. Scores include correct,
incorrect and missing counts per provider/language and overall. Accuracy uses valid
answers as its denominator; coverage uses all expected answers. Also inspect
correct/total and complete language-triplet consistency. Zero-denominator metrics
remain unknown. Consistency can include consistently incorrect choices and is not
accuracy. No per-case timing or token allocation is inferred from a batch.

A valid `insufficient_evidence` choice is scored against the oracle normally.
Refusals, malformed responses and unavailable providers remain missing coverage.
Incorrect choices do not fail execution: a fully answered run exits `0`, incomplete
provider coverage exits `2`, invalid inputs or output errors exit `1`, and an
interruption exits `130` while preserving completed batches. Existing run
directories are never overwritten. Credentials and raw transport exceptions are
excluded from reports.

The scorer does not choose an adviser or fill a task's `final_decision`.
Consultation records retain their pending decisions; evaluation scores are separate.
The coordinator interprets errors against the frozen facts and labels. A fixture defect
invalidates affected results; preserve the original run and correct the corpus as
a separate version rather than changing labels to match answers. A small synthetic
baseline does not establish calibrated confidence, production quality or a version
improvement. Offline resource tests exercise the runner with fake transports;
installation, source validation and native CI never invoke the hosted evaluation.

## Legacy Jev-only helper

`cw consult jev --request PATH --output PATH` remains supported for existing
Jev-only integrations. It accepts its existing request contract and uses the legacy
`consultation.model` policy field. The three-adviser `cw consult` uses `consultation.providers`
and `consultation.comparison: coordinator_evidence_and_verification` while preserving policy
version 2. Follow the installed TypeSafe guide for other primitives. A Jev-only result
does not provide three-provider coverage.
