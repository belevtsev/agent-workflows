# Checkpoints and adjudication

Read this when a checkpoint starts (states 2 and 4 of the
[decision flow](../SKILL.md#follow-the-decision-flow)) or before you adjudicate
any consultation. When `cw task show --field advisers` is not empty, also read
the [adviser guidance](adviser-providers.md) before the first consultation. With
an empty binding, skip it: the ledger records `no advisers configured` at the
spec, design and finding-rejection points, and at a checkpoint `cw consult`
makes no network call and writes a `no_advisers_configured` record, which you
adjudicate and attach under the same rules.

Contents:

- [Quick path](#quick-path)
- [Task checkpoints](#task-checkpoints)
- [Prepare a request](#prepare-a-request)
- [Records and exit codes](#records-and-exit-codes)
- [Adjudicate without network access](#adjudicate-without-network-access)
- [Attach to the task](#attach-to-the-task)
- [Sharing and recording](#sharing-and-recording)

## Quick path

A normal checkpoint needs only this section. `T` is the task ID, `P` the phase
(`before` or `final`); use new file paths for each phase.

1. Write `request.json` with exactly `state` and `questions` under the
   [authoring rules](question-patterns.md): Choice only, at most 8
   questions, 2 to 255 options with criteria, `insufficient_evidence` included,
   untrusted text in `state.untrusted`.
2. Embed the context and consult in one shell call:
   `cw task context --task T --phase P >context.json && jq --slurpfile c context.json '.state.task_context=$c[0]' request.json >request.final.json && cw consult --request request.final.json --output consult.json --summary`.
   Read `consult.json` only for a field the summary omits.
   Exit `5` means partial coverage (disclose it) or a bound adviser without its key
   (set it and consult again).
3. Check every judgment against source and write `decision.json` in the
   [decision shape](#adjudicate-without-network-access): entries are `source:`,
   `check:` or `observation:` and never name an adviser; give a reason for each
   differing adviser and a note for each differing one at confidence 0.7 or more.
4. Adjudicate and attach in one shell call:
   `cw consult --adjudicate consult.json --decision decision.json --output final.json && cw task consult --task T --phase P --input final.json`.

## Task checkpoints

When checkpoints are required ([state 1](../SKILL.md#follow-the-decision-flow)
says when), the before request assesses the proposed approach and verification plan; the final
request is new and assesses the completed work, current diff and checks before
completion or authorized publication. Use distinct requests and new output paths
for the two.

Reuse a consultation only for the same checkpoint with applicable evidence,
decisions, question meaning and models. If sharing is restricted, record that
limitation and decide on source evidence; do not bypass restrictions or add
retries.

A not-required reason must not claim that advisers are missing while the task
binds some; `cw task start` refuses such a plan. Its detection is a phrase
heuristic, so a reason about the task's own subject that matches, such as "the
change touches no provider credentials", must be reworded. The binding
(`cw task show --field advisers`, and `cw keys status` for the keys `cw` can use)
says which advisers are available, not `cw doctor`'s `credentials_launcher` or
`credentials_codex_shell` lines, which count other environments.

## Prepare a request

The input is JSON with exactly `state` and `questions`, under the
[authoring rules](question-patterns.md) and the
[question patterns](question-patterns.json). `cw consult` enforces the structure;
including `insufficient_evidence` is a rule it does not check. Mark text from
untrusted sources in `state.untrusted`, each entry with a non-blank `source` and
`text`:

```json
"untrusted": [{"source": "issue 412 comment", "text": "The cache is never invalidated after a deploy."}]
```

Verify excerpts against their exact sources before sharing them. The helper
compares returned choices; it cannot establish source truth or find omitted
evidence. To check a request without network access, preview it with
`cw consult --request request.json --output preview.json --dry-run`.

## Records and exit codes

A consultation record has version 4:
`{"version": 4, "status", "request", "request_sha256", "providers", "comparison", "final_decision"}`.
Its `status` is `no_advisers_configured` when no provider was attempted;
otherwise `success` when every attempted provider succeeded, `partial` when at
least one answered, and `unavailable` when none answered; a dry run is `dry_run`.
`final_decision` starts as `pending`.

- `0`: `success`, `no_advisers_configured`, `dry_run`, or a resolved adjudication.
- `5`: `partial` or `unavailable` coverage, an unresolved adjudication, or a bound
  provider without its key. The record or preview is still written.
- `2`: a usage error, such as an unknown or conflicting flag.
- `1`: an invalid request, record, decision or policy file, or an existing output path.

## Adjudicate without network access

The coordinator owns the final decision: `cw consult` never chooses an answer.
Investigate and independently verify each question against exact sources and
relevant checks, including when the advisers agree. Never select a winner by
confidence, probability, majority or provider identity; disagreement shows what
to investigate, and adviser output is never evidence. When a material fact is
missing or conflicting, leave the question unresolved with a reason naming the
gap. Retain the conclusion, evidence, verification, rejection reasons, unresolved
facts and adviser coverage in the task record. Advice and confidence never grant
authorization.

```sh
cw consult --adjudicate consultation.json --decision decision.json --output final.json
```

Adjudication makes no provider requests and writes a new record. The decision
names every question ID exactly once; this shape is to adapt, not evidence:

```json
{"questions": {
  "write_owner": {
    "status": "resolved", "choice": "single_writer",
    "reason": "Only the store package writes the table; the handler reads through it.",
    "evidence": ["source:internal/store/write.go:40-58@3f2a9c1"],
    "verification": ["check:unit", "observation:rg finds no other INSERT into the table at 3f2a9c1"],
    "rejected_judgments": {"subq_snap": "The cited migration runs once at deploy and does not write at runtime."},
    "investigations": {"subq_snap": "Confidence 0.82; migrations/0042.sql:1-30 is deploy-only."},
    "agreement_not_independent": false
  },
  "retry_budget": {
    "status": "unresolved", "choice": null,
    "reason": "No available source documents the upstream timeout.",
    "evidence": [], "verification": [], "rejected_judgments": {}, "investigations": {},
    "agreement_not_independent": false
  }
}}
```

- **Every item** has exactly these eight keys; `reason` is non-blank.
- **Resolved:** `choice` is an allowed option other than `insufficient_evidence`.
  `evidence` and `verification` each have at least one entry, and each entry is
  exactly one of `source:<path>:<start>[-<end>]@<revision>` (a clean relative
  path; the revision is 7 to 64 lowercase hex characters or `sha256:` plus 64),
  `check:<check-id>[@<run-id>]`, or `observation:<text>`.
- **No adviser names:** check IDs, run IDs and observations must not contain
  `jev`, `snap`, `subq`, `subq_snap`, `openai_decisions`, `openai decisions`,
  `gpt-6-luna`, `typesafe`, `adviser`, `advisers`, `advisor` or `advisors` as a
  whole word in any case; any character other than a letter or digit, including
  `_`, separates words. Name planned checks without those words: `snap-test`
  cannot be cited, `snapshot-tests` can. `source:` paths are exempt.
- **Rejected judgments:** keys are exactly the providers whose recorded choice
  differs from `choice`, each with a non-blank reason, in a `partial` comparison
  as in `disagreement`. Missing or refused judgments need none.
- **Investigations:** keys include every differing provider whose answer
  confidence is 0.7 or higher, each with a non-blank note, and no other provider.
- **Untrusted agreement:** with `state.untrusted` in the request and an
  `agreement` comparison, `agreement_not_independent` is true; it may be true on
  any other resolved item.
- **Unresolved:** `choice` is null, `rejected_judgments` and `investigations`
  are empty, `agreement_not_independent` is false, and `reason` names the gap.

The final record is `resolved` only when every question is. These rules apply in
full to a `no_advisers` comparison. Validation checks that the coordinator
recorded its obligations; it cannot prove the evidence or reasoning correct.

## Attach to the task

`cw task consult --task T --phase before|final --input final.json` revalidates
the record against the task's binding and rejects a `not_configured` bound
provider. Attaching again for the same phase replaces the earlier attachment,
when the input is a new file distinct from the other phase's: after an unresolved
decision, adjudicate again, or consult again with new evidence, then attach.

With required checkpoints, `cw task assign` waits for a resolved before
checkpoint and `cw task verify` needs both; a before record never satisfies the
final phase. With `"required": false`, verify lists the reason as a limitation.
When verify reports that this reason claims no advisers while the task binds
some, do what its message says; never start a new record over source the first
one changed without asking the user.

`cw` reads task records version 3 to 5 and consultation records version 4: restart
an older task with `cw task start`, and upgrade `cw` for a record from a newer one.

## Sharing and recording

The record excludes credentials, authorization headers, raw errors and unknown
response fields, but keeps the request verbatim. Sanitize inputs before sharing or
recording them: the helper cannot remove credentials or sensitive material
embedded in evidence. Keep records in the task's permitted storage; saving outside
the agreed task location requires its own scope.
