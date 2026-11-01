---
name: task-orchestration
description: Coordinate substantial work with bounded workers, grounded assumptions, required Jev, SubQ Snap and OpenAI Decisions advice, and verification tied to the revision.
---

# Task orchestration

The coordinator owns the outcome, consultation, dispatch, integration, and final
verification. Use this skill for independent workstreams or consequential
uncertainty; keep small or dependent work with the main worker. Specialist evidence
requirements, repository instructions, and the user's authorization still govern.

## Establish policy, scope, and evidence

Read [model-policy.yaml](model-policy.yaml) and the relevant section of
[use cases](references/use-cases.md). Under Claude Code, also read
[model-policy.claude.yaml](model-policy.claude.yaml), which replaces the profiles and
roles, and [host mapping](references/hosts.md), which covers dispatch and paths. Where
this skill names Sol and Luna below, use the matching Claude Code profile. Read [connected sources](references/connected-sources.md)
only when Jira, Confluence, GitHub, or documentation supplies evidence or requested
writes. Fetch linked evidence needed for the decision; unrelated services are not
prerequisites for local work.

The YAML is configurable dispatch policy, not native host configuration. Codex
defaults are Sol 6.1 `max` for substantive work, Luna `high` for bounded evidence, and Luna
`xhigh` for bounded routine execution. Resolve each role's profile and scope from
the current policy, preserving explicit user choices. Sol `max` may be configured
or explicitly requested; Sol `ultra` needs an explicit user request allowed by the
profile. Never escalate automatically. A saved policy cannot switch an active
session. Verify model and effort from runtime metadata; disclose a mismatch. Without
an explicit user override, substantive coordination and final verification require
a compatible main worker. If unavailable, retain the brief and evidence, name the
required fresh-session configuration, and continue only bounded evidence gathering.
A compatible subworker does not repair the main worker's mismatch.

Establish the outcome, acceptance evidence, scope, carried authorization, revisions,
and available tools. Keep requirements and user decisions separate from observations
and assumptions. In the agreed artifact or chat record retain a compact ledger:
`claim | basis/source revision | status | impact | next check`, using `unverified`,
`verified`, `contradicted`, or `obsolete`. Retain assignments, completed checks,
consultation disposition, and blockers; a preset never verifies its own claims.

For substantial repository work, use the installed native `cw doctor` and `cw task`
commands following [global workflow](references/global-workflow.md). They apply to
the selected project, independently of the manager's installation source. Keep
project commands and deployment rules in that repository's instructions. Bind each
consultation to the emitted task context, execute only explicitly selected checks,
and run task verification before completion. The native record checks identity and
evidence consistency; the coordinator still verifies meaning and omitted facts. For work
spanning repositories, retain one source-bound record per repository and batch
compatible contexts into the same adviser request. Non-repository tasks retain the
chat/artifact ledger above; small tasks remain lightweight. Use `cw metrics` for
observed outcomes, preserving unknowns and the distinction between synthetic
acceptance, actual diagnostics, and productivity evidence.

## Consult advisers centrally

Resolve installed `typesafe-ai`; read its entrypoint, development consultation
guide, selected-model limitations, and relevant primitive documentation. Read the
current SubQ [Decisions API](https://docs.subq.ai/endpoints/decisions-api/),
[models](https://docs.subq.ai/using-the-api/models/), and
[retention policy](https://docs.subq.ai/using-the-api/data-retention/) before sharing
evidence with Snap.
For OpenAI Decisions, read its current
[guide](https://developers.openai.com/api/docs/guides/decisions),
[API contract](https://developers.openai.com/api/reference/resources/decisions/methods/create),
and applicable [data controls](https://developers.openai.com/api/docs/guides/your-data).

For every substantial task that activates this skill, all three advisers are
mandatory at both checkpoints:

1. **Before implementation:** gather enough source evidence to assess the proposed
   approach, alternatives, risks, and verification plan, then consult all three providers
   before implementation edits or dispatching implementation workers. For a task
   without implementation, consult before substantive drafting or review.
2. **During final review:** consult all three providers again after integration and the
   relevant checks, before claiming completion or making an authorized publication.
   Use the exact completed work, current revision or diff, check results, and
   remaining limitations to assess acceptance, regressions, and evidence gaps.

These checkpoints are separate evidence batches. Always prepare a fresh final-review
request; pre-implementation advice cannot satisfy it. Reuse advice only within the
same checkpoint while its evidence, decisions, question meaning, and models remain
applicable. Routine turns do not require additional calls, but material changes
refresh affected consultations. Settled scope, model, and publication rules remain
constraints; ask about the approach or evidence rather than reconsidering those rules.
The coordinator must not skip a checkpoint because an approach seems obvious or
earlier advice was favorable. Workers return evidence and questions rather than
duplicate calls.

The policy requires `jev: jev-1.14.0`, `subq_snap: subq-snap-preview` and
`openai_decisions: gpt-6-luna` together under `consultation.providers`.
All three have equal advisory standing and receive the same permitted evidence.
Supply `TYPESAFE_API_KEY`, `SUBQ_API_KEY` and `OPENAI_API_KEY`; a missing key is
unavailable required coverage. The helper cannot silently disable a required
adviser, and `--without-openai` is rejected under this policy. An OpenAI Decisions
call does not dispatch a Luna worker or inherit its reasoning effort.

Each checkpoint requires bounded attempts to all three providers within permitted
sharing. If a provider, credential, dependency, service, or permitted sharing is
unavailable, record the checkpoint, reason, and coverage; never bypass restrictions
or invent a response. Continue source investigation and independent review under
the existing fallback, disclose the limitation, and keep material unresolved facts
open. Provider unavailability alone does not block completion or justify retries.
Record both checkpoint dispositions in the task record.

Use [question patterns](references/jev-questions.json) with exact evidence,
constraints, and viable alternatives. Participating advisers receive the same permitted
minimal text evidence, questions, and options; only the model and endpoint differ.
Record the source revision and confirm excerpts match that revision before sending.
No adviser has implicit repository or history access. They supply typed advice,
not code, findings, explanations, assumptions, proof, or permission. The common
workflow is Choice-only and must fit the participating providers' limits; Snap's larger context
and image support do not expand this shared request contract.

Use the policy's `jev-1.14.0`, `subq-snap-preview`, and `gpt-6-luna` model IDs.
OpenAI uses `POST /v1/decisions`; its adapter maps the shared evidence and Choice
questions to named arrays and validates the returned answers independently.
Refusals are missing coverage for the affected question; preserve other valid
answers and their rejection obligations. Snap's ID is a public
preview alias, not an invented immutable version; record the returned model and
recheck its contract when it changes. Batch independent questions sharing evidence
and send one request to each provider concurrently with `cw consult`, within a
30-second batch budget covering connections, requests, and complete responses. A
provider still running at the deadline is cancelled, and automatic retries are disabled.
Validate response IDs, types, allowed options,
finite probabilities, distributions, and resolved model independently. Separate
`TYPESAFE_API_KEY`, `SUBQ_API_KEY`, and `OPENAI_API_KEY` credentials remain outside inputs and records.
For its request, output, and privacy contract, read
[consultation helper](references/consultation-helper.md) before using it.

The coordinator owns the final decision. Compare all participating judgments against the exact
source and relevant tests or observations, even when they agree. Do not select a winner by
confidence, probability, majority vote, or provider identity. Disagreement identifies
what to investigate; invalid or insufficient advice remains an investigation lead.
In the agreed task record retain the conclusion, supporting evidence references and
verification, reasons for rejecting a judgment, unresolved facts, and provider coverage.
The helper initializes `final_decision` as `pending`; it never chooses an answer.
After verification, use the no-network `cw consult --adjudicate` mode to validate the coordinator's
disposition and write a new final record, preserving the original provider record.
If a required provider is unavailable or refuses a question, continue source work
and disclose missing coverage. Do not describe a partial consultation as successful
three-provider coverage. Mandatory-three records use version three and reject
disabled required providers. Saved version-one pair and version-two optional-adviser
records remain valid for offline adjudication without rewriting the originals.
If a material fact is missing or conflicting, leave the final decision unresolved.
Advice and confidence never grant authorization.

## Dispatch bounded briefs and integrate

Substantive coding, testing, planning, review, diagnosis, documentation, new
acceptance claims, and final verification use the configured non-Luna substantive
profile. Routine execution requires fixed files, revision, content, and action
authorization from the coordinator. New claims, ambiguous scope, conflicts, or
uncertain verification return to substantive ownership. Model selection never
relaxes a role's action bounds.

Check actual runtime capabilities before spawning. When overrides are supported,
supply model and effort explicitly. Under Codex, use `fork_turns="none"`: a
full-history fork inherits the parent and rejects overrides. Under Claude Code,
follow the [host mapping](references/hosts.md). Respect fixed specialist models
and use them only within their evidence role. If overrides are unsupported, use a
compatible inherited configuration or report the limit. Follow the policy's
bounded-worker fallback; never silently lower a substantive effort or model family.
Native `cw validate` checks consistency, not runtime availability or automatic
dispatch. A role's profile gives its configured model and effort; an explicit user
request can override that effort only with a value the profile lists in
`user_requested_efforts`. Inspect the role's scope before using a profile for dispatch.

Split useful independent questions or owned components. Give each worker a
self-contained brief with exact source revision and inputs, question or owned
files, relevant decisions, constraints, permitted actions, output evidence, and
stopping condition. Include only context needed for that assignment; avoid full
history dumps and duplicate investigation. Editing workers share the codebase and
must preserve others' changes. Coordinate shared interfaces and respect concurrency
limits; dependent integration stays with the coordinator.

Workers return exact sources, observations, changes, checks, and uncertainty.
Reconcile overlaps and independently verify conclusions; agreement and confidence
do not establish behavior. Preserve required design challenges and execution
environments.

For repository workers, use the native [worker handoff contract](references/worker-handoffs.md)
to record assignments, results, and coordinator acceptance. Create the assignment
before dispatch and give the worker the emitted source binding and result contract.
These commands validate records; dispatch and runtime identity verification remain
the coordinator's responsibility. They do not expand a worker's permissions.

Substantial source changes require an independent substantive worker review before
the final adviser checkpoint. Give the reviewer the completed source, relevant
contracts, and selected check results. It must be a different agent from the
coordinator and implementation workers. Record each finding and its evidence-backed
disposition; the coordinator can reject a finding with a reason. Review and check execution may
overlap once source is stable. Later source changes require a current review and
refresh only affected checks and acceptance evidence. The three adviser APIs remain
separate required checkpoints. Missing independent review cannot be presented as
completed review. Small and unchanged read-only tasks stay lightweight.

## Refresh affected claims and complete

When scope, revisions, acceptance text, environment, findings, or checks materially
change, update only dependent ledger entries, assignments, and consultations. Keep
user decisions and authorization. A new turn alone does not invalidate evidence.

Prepare requested external drafts from verified evidence, carry existing
authorization, and verify writes through readback. Reconcile uncertain outcomes
before retrying. Finish with the completed result and evidence at its actual scope:
structural fixtures, fresh discovery, model advice, tests, CI, and deployment remain
distinct. Mention materially used Jev/Snap/OpenAI advice and providers that were
unavailable; identify the coordinator's independently supported decision.
