# Decision consultation helpers

## Quick path

A normal checkpoint needs only this section. `T` is the task directory, `P` the
phase (`before` or `final`); use new file paths for each phase.

1. If `adviser-terms-reviewed.md` in the state directory (default
   `~/.local/state/agent-workflows/`) is missing, older than 30
   days, or names another policy version or provider set, read the
   [provider contracts](adviser-providers.md#provider-contracts) and update it.
2. Write `request.json` with exactly `state` and `questions` under the
   [authoring rules](question-patterns.md): Choice only, at most 8
   questions, 2 to 255 options with criteria, `insufficient_evidence` included,
   untrusted text in `state.untrusted`.
3. Embed the context and consult in one shell call:
   `cw task context --task T --phase P >context.json && jq --slurpfile c context.json '.state.task_context=$c[0]' request.json >request.final.json && cw consult --request request.final.json --output consult.json --summary`.
   The summary prints each adviser's answers and the comparison and names the
   request file instead of repeating it; read `consult.json` only for a field it omits.
   Exit `2` means partial coverage (disclose it) or a bound adviser without its key
   (set it and consult again).
4. Check every judgment against source and write `decision.json` in the
   [decision shape](#adjudicate-without-network-access): entries are `source:`,
   `check:` or `observation:` and never name an adviser; give a reason for each
   differing adviser and a note for each differing one at confidence 0.7 or more.
5. Adjudicate and attach in one shell call:
   `cw consult --adjudicate consult.json --decision decision.json --output final.json && cw task consult --task T --phase P --input final.json --summary`.

Read the rest of this file only for a case the quick path does not cover.
It validates a prepared Choice-only request, sends the same shared input to every
configured adviser concurrently, and records each result independently. It does
not dispatch workers or decide which judgment is correct. The command is part of
the native `cw` command and needs no interpreter or package installation.
Provider-specific details, data handling and the multilingual evaluation are in
[adviser providers](adviser-providers.md); a checkpoint does not need them.

## Task checkpoints

Substantial tasks follow the fixed flow in the [entrypoint](../SKILL.md#follow-the-decision-flow):
classify, before checkpoint, implement and verify, fresh final checkpoint, and
complete only when both checkpoints are resolved. Checkpoints are required when
the task has an open decision; a plan whose approach follows directly from the
defect or contract, with no changed code that must behave alike on several
platforms or shells, declares `"checkpoints": {"required": false, "reason": "..."}`
and skips them; when in doubt, keep them required. No required state is skipped. Prepare distinct requests and new
output paths for the two checkpoints. The before request
assesses the proposed approach and verification plan; the final request assesses
the completed work and current checks before completion or authorized publication.
Put the emitted task context in request state and retain adviser coverage and the
coordinator's disposition in the task record.

A not-required reason must not claim that advisers are missing when the task binds
any: `cw task start` refuses such a plan, and `cw task verify` reports it on a record
an older `cw` created (see [Attach to the task](#attach-to-the-task)). Detection is a
closed phrase heuristic with known false negatives and false positives; a reason
about the task's own subject that matches, such as "the change touches no provider
credentials", must be reworded. The binding (`cw task show --field advisers`, and
`cw keys status` for the keys `cw` can use) says which advisers are available, not
`cw doctor`'s `credentials_launcher` or `credentials_codex_shell` lines, which count
other environments.

Reuse is limited to the same checkpoint with applicable evidence, decisions, question
meaning and models. A before record cannot satisfy the final checkpoint. If sharing
is restricted, record that limitation and follow the source-evidence fallback; do
not bypass restrictions or add retries. Installation, validation and CI do not
call hosted models.

## Adviser points in the flow

Once a task has started, and after the before checkpoint when its checkpoints
are required (unchanged, [above](#task-checkpoints)), advisers enter the
[flow](contracts/README.md#flow) at four decision points only, never at every
step:

| Point | When | Record |
| --- | --- | --- |
| a | After the spec review, on each open question the spec leaves to a choice | interim consultation |
| b | On the architect's choice between real options, before the design review | interim consultation |
| c | Before the coordinator rejects a `blocker` or `major` finding at any [review stage](worker-handoffs.md#review-stages) | interim consultation |
| d | The final checkpoint before `cw task verify` | the task's `final` checkpoint ([above](#task-checkpoints)) |

An adviser is available when the policy enables it, the user has not disabled it
and its credential is present: the [participation](#participation) rule, which
`cw task start` records as the task's `advisers` binding (`cw task show --field advisers`;
`cw doctor` and `cw keys status` report missing credentials). With any adviser
bound, the coordinator consults at each point. With none bound, points a to c
are skipped and the ledger records `no advisers configured`, never a failure;
at point d, when checkpoints are required, `cw consult` makes no network call
and writes the `no_advisers_configured` record that the checkpoint attaches as
usual. The flow then runs exactly as it does without advisers. An adviser that
fails or refuses is recorded `unavailable` or `refusal` and blocks nothing.

An interim consultation (points a to c) is an ad hoc request: follow the
[quick path](#quick-path) without `cw task context` in step 3 (the before
context matches only the baseline source), pass the `--policy` the task started
with, if any, and stop after `cw consult --adjudicate`: there is no task phase
to attach it to. Ad hoc participation applies the same three conditions, so it
matches the binding unless the policy, preferences or keys changed since
`cw task start`; consult only when the binding is not empty. Keep its files with the task's other private records, outside
source, and cite its path and outcome in the ledger, or for point c in the
finding's disposition `reason`. Its evidence stays source, checks and
observations under the [adjudication rules](#adjudicate-without-network-access).
Point a's decisions reach the spec writer as the coordinator's answers, and
point b's reach the architect, never the adviser output itself.

Inside a substantial task these points replace the suggestions in specialist
skills (such as `code-review`, `test-strategy`, `software-architecture`,
`production-plan`, `principal-engineer` and `security-threat-model`) to consult
advisers on a consequential judgment: such a judgment waits for the next point,
or is point c when it is the rejection of a finding. Outside a substantial task
those skills keep their own rule.

## Participation

An adviser participates only when it is configured: enabled in the policy's
`consultation.providers` with its pinned model, with its credential set in the
environment or saved credentials, opted in to, and not disabled in the user's
preferences. The user opts in by saving the key with `cw keys set`, by listing the
provider ID under `advisers.enabled` in `preferences.json`, or by passing
`--policy`, which opts in to every provider that policy enables; a credential in
the environment alone binds nothing, and `cw doctor` warns "key present, adviser
not enabled". To
disable one and keep its key, list its provider ID under `advisers.disabled` in
`preferences.json` in the configuration directory, edited by hand. The selected
policy then omits it, and `cw task start` and ad hoc `cw consult` skip it
even with a `--policy` copy; started tasks keep their binding.

| Provider ID | Pinned model | Credential |
| --- | --- | --- |
| `jev` | exact `jev-<major>.<minor>.<patch>`, such as `jev-1.14.0` | `TYPESAFE_API_KEY` |
| `subq_snap` | `subq-snap-preview` | `SUBQ_API_KEY` |
| `openai_decisions` | `gpt-6-luna` | `OPENAI_API_KEY` |

The shipped policy (version 3) enables `subq_snap` and `openai_decisions`; Jev is
opt-in ([why](adviser-providers.md#why-jev-is-opt-in)). Any other policy version,
unknown provider IDs and moving aliases such as `jev-latest` are rejected. An
OpenAI Decisions call does not dispatch a Luna worker or inherit its reasoning
effort.

The participation set comes from one place:

- **Task-bound request:** when `state.task_context` is present, it must hold the
  `advisers` binding that `cw task start` recorded, and the participation set is
  exactly that binding. With `state.task_contexts` (several repositories), every
  context must carry an identical binding. A context without a binding is
  rejected; regenerate it with this `cw task context`. A `state` holding both
  `task_context` and `task_contexts` is rejected.
- **Ad hoc request:** otherwise, the policy's enabled providers participate; one
  disabled in the saved preferences, or without `--policy` one not opted in to, is
  recorded as `not_configured` and never contacted.

Without `--policy PATH`, `cw task start` and `cw consult` use `$CW_MODEL_POLICY`,
then `~/.agents/skills/task-orchestration/model-policy.yaml` when present, then the
policy embedded in `cw`. `cw task start`
binds the providers enabled in that policy whose credentials are present, that
the user opted in to and that the saved preferences do not disable, read through
the same environment-or-saved
credentials as `cw consult`. If the policy or the saved preferences cannot be read
or are invalid, including an older policy version, `cw task start` fails before
creating a task directory; it never binds an empty set silently. A valid
policy whose enabled providers all lack credentials binds `{}`.

### Opt in to Jev

1. Copy `skills/task-orchestration/model-policy.yaml` from the repository to a
   private location.
2. Add `jev: jev-1.14.0` under `consultation.providers` in the copy; if it reads
   `providers: {}`, replace `{}` with that mapping.
3. Set `TYPESAFE_API_KEY`.
4. Start the task with `cw task start --policy /path/to/copy.yaml`. Its checkpoints
   then follow the task's binding automatically; `cw consult` needs no `--policy`
   for task-bound requests.

For ad hoc consultations, pass the copy with `cw consult --policy /path/to/copy.yaml`.
A copy still honours `advisers.disabled` in the saved preferences.

A participating provider without its credential is recorded as `not_configured`
and receives no request. When nothing participates, or every participant is
`not_configured`, `cw consult` makes no network call and writes a record with status
`no_advisers_configured`, adjudicated and attached under the same rules.
Unavailability or refusal alone neither blocks completion nor justifies retries;
disclose it.

## Prepare and run a shared request

The input is JSON with exactly `state` and `questions`. Follow the
[authoring rules](question-patterns.md) and the provider-neutral
[question patterns](question-patterns.json). The helper enforces the structure:
Choice questions only, at most 8 per request, and 2 to 255 named options each.
Including `insufficient_evidence` is a documented rule, not a check.

Mark text from untrusted sources in an optional `state.untrusted` array. When present
it must be non-empty, and each entry needs non-blank `source` and `text`:

```json
"untrusted": [
  {"source": "issue 412 comment", "text": "The cache is never invalidated after a deploy."}
]
```

The coordinator verifies excerpts against their exact sources before sharing them.
The helper can validate structure and compare returned choices; it cannot establish
source truth or discover omitted evidence. Every participating provider receives the
same state, instructions and criteria; the helper supplies the pinned model IDs.
The common workflow has no images or other primitives.

Preview the request, then send it:

```sh
cw consult --request /path/to/request.json --output /path/to/preview.json --dry-run
cw consult --request /path/to/request.json --output /path/to/new-record.json
```

`--policy /path/to/model-policy.yaml` selects a validated policy. Without it,
`cw consult` uses `$CW_MODEL_POLICY`, then
`~/.agents/skills/task-orchestration/model-policy.yaml` when present, then the
policy embedded in `cw`. `--dry-run`
validates the policy and input and writes a preview without network access; it lists
each participating provider with its `requested_model` and status `dry_run` or
`not_configured`, resolving credentials only to report that status. A task-bound
dry run whose bound adviser lacks its credential still writes the preview and
exits `2` with the same message as a real run.

A real call makes one attempt per configured provider. A 30-second batch budget
covers connecting, sending and reading every complete response; a provider still
running at the deadline is cancelled and recorded as a timeout. It neither retries
nor substitutes another model. One provider's failure preserves the others' results.
Responses are validated for IDs, types, options, finite values, distributions,
usage and returned model.

## Records

A consultation record has version 4:
`{"version": 4, "status", "request", "request_sha256", "providers", "comparison", "final_decision"}`.
Any other version is rejected. `providers` holds exactly the participation set, in
the order `jev`, `subq_snap`, `openai_decisions`. Each provider has one status:

| Status | Meaning |
| --- | --- |
| `success` | Valid answers to every question |
| `partial` | OpenAI answered some questions and refused others |
| `refusal` | OpenAI refused every question |
| `unavailable` | Attempted and failed: `timeout`, `http_error` (with `http_status`), `network_error`, `invalid_response`, `worker_error` or, for OpenAI, `unsupported_request` |
| `not_configured` | Participating, but no credential; no request was made |

The record `status` is `no_advisers_configured` when no provider was attempted;
otherwise `success` when every attempted provider succeeded, `partial` when at least
one answered, and `unavailable` when none answered. A dry run has status `dry_run`.

Each question's comparison holds `status`, `choices` (each answering provider's
choice) and `missing` (every other participating provider, with its status, or
`refusal` for a refused question). The comparison status is `no_advisers` when
nothing was attempted, `unavailable` when there is no choice, `agreement` when every
attempted provider chose the same option, `disagreement` when every attempted
provider chose and the choices differ, and `partial` otherwise. These states describe
coverage and choice equality, not truth. `final_decision` starts as `pending`.

Exit codes:

- `0`: `success`, `no_advisers_configured`, `dry_run`, or a resolved adjudication.
- `2`: `partial` or `unavailable` coverage, an unresolved adjudication, or a
  task-bound request or dry run where a bound provider is `not_configured`. The
  record or preview is still written, and stderr says:
  `Adviser <id> is bound to this task but not configured; set <VAR> and consult again.`
- `2`: a command-line usage error, as for every `cw` command: an unknown flag (such
  as the removed `--with-openai` or `--without-openai`), or conflicting or missing
  options. Usage errors are reported before any file is read.
- `1`: an invalid input file (request, record, decision or policy) or an existing
  output path.

## Adjudicate without network access

The coordinator owns the final decision: `cw consult` leaves `final_decision`
`pending` and never chooses an answer. The coordinator investigates and
independently verifies each question against exact sources and relevant checks,
including when the advisers agree, then writes a decision JSON and validates it
against the original consultation record. Never select a winner by confidence,
probability, majority or provider identity; disagreement shows what to
investigate, and adviser output is never evidence. When a material fact is missing
or conflicting, leave the question unresolved with a reason naming the gap. Retain the
conclusion, evidence, verification, rejection reasons, unresolved facts and adviser
coverage in the task record. Advice and confidence never grant authorization.

```sh
cw consult --adjudicate /path/to/consultation.json --decision /path/to/decision.json --output /path/to/final.json
```

`--adjudicate` and `--request` are mutually exclusive. Adjudication makes no provider
requests and uses no credentials. It writes a new record, preserving the
consultation input and provider results. The decision contains every question ID
exactly once:

```json
{
  "questions": {
    "write_owner": {
      "status": "resolved",
      "choice": "single_writer",
      "reason": "Only the store package writes the table; the handler reads through it.",
      "evidence": ["source:internal/store/write.go:40-58@3f2a9c1"],
      "verification": [
        "check:unit",
        "observation:rg finds no other INSERT into the table at 3f2a9c1"
      ],
      "rejected_judgments": {
        "subq_snap": "The cited migration runs once at deploy and does not write at runtime."
      },
      "investigations": {
        "subq_snap": "Confidence 0.82; migrations/0042.sql:1-30 is deploy-only."
      },
      "agreement_not_independent": false
    },
    "retry_budget": {
      "status": "unresolved",
      "choice": null,
      "reason": "No available source documents the upstream timeout.",
      "evidence": [],
      "verification": [],
      "rejected_judgments": {},
      "investigations": {},
      "agreement_not_independent": false
    }
  }
}
```

This example is a shape to adapt, not evidence for a real decision. Each item has
exactly `status`, `choice`, `reason`, `evidence`, `verification`,
`rejected_judgments`, `investigations` and `agreement_not_independent`. The rules:

- **Every item:** `reason` is non-blank and `agreement_not_independent` is a boolean.
- **Resolved:** `choice` is an allowed option other than `insufficient_evidence`.
  `evidence` and `verification` each have at least one entry, and every entry is
  exactly one of:
  - `source:<path>:<start>[-<end>]@<revision>`: a clean relative path, positive
    lines with end at least start, and a revision of 7 to 64 lowercase hex
    characters or `sha256:` plus 64 hex characters;
  - `check:<check-id>[@<run-id>]`: non-blank IDs without whitespace that do not
    name an adviser;
  - `observation:<text>`: non-blank text that does not name an adviser.

  "Names an adviser" means containing `jev`, `snap`, `subq`, `subq_snap`,
  `openai_decisions`, `openai decisions`, `gpt-6-luna`, `typesafe`, `adviser`,
  `advisers`, `advisor` or `advisors` as a whole word in any case. A word boundary
  is the start or end of the text or any character other than a letter or digit,
  so an underscore separates words. The rule covers check IDs, run IDs and
  observation text: `check:subq_snap`, `check:jev@x`, `check:unit@openai_decisions`,
  `check:jev_answer` and `check:subq_snap_choice` are rejected, while
  `check:go-test-snapshot` and `check:snapshot-tests` are accepted. A planned check
  whose ID contains an adviser word, such as `snap-test`, `advisor-lint` or
  `jev_smoke`, cannot be cited, so name planned checks without those words. `source:` paths are exempt, because
  they cite repository files such as `skills/typesafe-ai/SKILL.md`. Any other
  entry, or one that names an adviser, is rejected: evidence and verification must
  reference source, checks or observations, not adviser output.
- **Rejected judgments:** keys are exactly the providers whose recorded choice
  differs from `choice`, each with a non-blank reason. This applies to a `partial`
  comparison whose recorded choices differ exactly as to `disagreement`. Missing
  or refused judgments need none.
- **Investigations:** keys are a subset of the differing providers and must include
  every one whose answer confidence is 0.7 or higher, each with a non-blank note,
  in a `partial` comparison as in `disagreement`. A missing note is rejected,
  naming the provider and its confidence.
- **Untrusted agreement:** when the request has `state.untrusted` and the question's
  comparison is `agreement`, `agreement_not_independent` must be true. It may be
  true on any other resolved item.
- **Unresolved:** `choice` is null, `rejected_judgments` and `investigations` are
  empty, `agreement_not_independent` is false, and `reason` names the material gap.
  `evidence` and `verification` may be empty, but each listed entry must be valid.

The final record is `resolved` only when every question is resolved. These rules
apply in full to a `no_advisers` comparison. Structural validation checks that the
coordinator recorded its obligations; it cannot prove the cited evidence or the
reasoning correct.

## Attach to the task

`cw task consult --task DIR --phase before|final --input FINAL.json` revalidates the
record, then requires its providers and requested models to equal the task's
`advisers` binding. Every bound provider needs `success`, `partial`, `refusal` or
`unavailable`; a `not_configured` provider is rejected and named. With an empty
binding, the record status must be `no_advisers_configured`.

Attaching again for the same phase replaces the earlier attachment, as long as the
input is a new file distinct from the other phase's artifact. Use this after
resolving an unresolved decision: adjudicate again, or consult again with new
evidence, then attach the new final record.

For a substantial task with required checkpoints, `cw task assign` is refused
until the before checkpoint is attached and resolved, `cw task verify` requires
both checkpoints resolved, and a before record never satisfies the final phase.
With `"required": false`, verify lists the reason as a limitation, and an attached
checkpoint is still validated. If that reason claims no advisers are configured while
the task binds some, and the two checkpoints are not both attached, verify adds one
problem, `plan checkpoints reason claims no advisers are configured, but this task
binds <IDs>; ` followed by:

- `attach the final checkpoint` when a usable before checkpoint is attached:
  resolved and valid against the baseline, as the final context requires;
- `attach the before and final checkpoints, or abandon this record and start a
  corrected one` when there is no usable before checkpoint and the source is
  unchanged since start;
- `the source changed since start, so the before checkpoint can no longer be
  attached and a new record must not start over the changed source: stop and ask
  the user how to proceed` when there is no usable before checkpoint and the
  source changed.

With an empty binding, verify reports that no advisers were configured when the
task started, so decisions rest on source evidence and checks only. This release
reads task record version 3 and consultation record version 4 only; task records
and consultation records from earlier releases do not load, so restart such a task
with `cw task start`.

## Sharing and recording

The helper writes only to the supplied new output path. Its record excludes
credentials, authorization headers, raw errors and unknown response fields, but
retains the prepared request verbatim. Sanitize inputs before sharing or recording
them; the helper cannot remove credentials or sensitive material embedded in
evidence. Keep sensitive records in the task's permitted storage. Saving outside the
agreed task location requires its own scope.

Before configuring any adviser, confirm that sending source excerpts to that
provider is permitted for the code and organization involved. Each provider's data
handling is summarized in [adviser providers](adviser-providers.md#data-handling).
