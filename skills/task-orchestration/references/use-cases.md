# Task presets

Read only the section matching the deliverable. Presets suggest workstreams; they
never authorize implementation, publication, or operations. Resolve model, effort,
and role scope from [model-policy.yaml](../model-policy.yaml), following the
[entrypoint](../SKILL.md)'s runtime and authorization safeguards.

Defaults are the `sol` profile at `high` for substantive work and `decision` at `max`
for planning, architecture and diagnosis, `luna` at `high` for bounded evidence,
`github` at `high` for GitHub execution, and `luna_execution` at `xhigh` for other bounded execution.
Claude's GitHub execution default is `sonnet` at `medium`. `max` on `sol` can be
configured or explicitly requested; `ultra` requires an explicit permitted user request.
Automatic escalation stays disabled. Configuration does not switch active sessions
or prove depth, cost, or savings.

Split independent questions, owned components, or evidence sources. Brief each
worker with exact inputs/revision, question or files, constraints and permitted
actions, expected evidence, and stopping condition. Pass only relevant context;
avoid duplicate investigation. Dependent integration stays with the coordinator.
Resolve installed specialists from the current catalog. Repository commands,
execution environments, and product conventions belong in repository instructions.

Every substantial preset follows the entrypoint's [decision flow](../SKILL.md#follow-the-decision-flow).
When the task has an open decision (real alternatives with consequential
uncertainty), it runs a before checkpoint ahead of implementation or substantive
drafting/review, and a fresh final checkpoint on the completed deliverable and
current evidence before completion or publication. Each checkpoint consults the
task's configured advisers, if any, and needs a resolved adjudication; earlier
advice cannot replace the final one. Record each checkpoint's disposition,
including advisers that were not configured, unavailable or restricted. When the
approach follows directly from the defect or contract and no changed code must
behave alike on several platforms or shells, the plan declares
`"checkpoints": {"required": false, "reason": "..."}` instead; when in doubt, keep
them required.

## Implementation

When a substantial task adds or materially changes behavioral tests, or the user
asks, dispatch the independent [test engineer](dispatching-reviews-and-tests.md#dispatch-a-test-engineer). Agree the
versioned contract and interfaces first, then separate exact implementation and
test-artifact ownership. Authors receive approved contracts/scaffolding only;
source-aware coordination or verification owns execution and filtered feedback.
A valid red-test handoff completes authorship, not verification. Return semantic
assertion/fixture/helper/skip changes to an unexposed author and retain patch
identity. A user-requested author on a small task does not activate substantial
checkpoints; otherwise the coder writes the tests and the reviewer checks each
against the Code Review Rules.

Establish behavior, interfaces, and observable acceptance. Give workers exclusive
file ownership and agree shared interfaces before edits; workers must accommodate
others' changes. Integrate and run required checks in the repository environment,
fixing failures caused by the change. Substantial source changes require an
independent substantive worker review of the completed source, recorded through
the [worker handoff contract](worker-handoffs.md) before the final adviser checkpoint.
The coordinator resolves, defers or rejects each finding against evidence and
stays within the [review cap](worker-handoffs.md#review-stages);
keep small and unchanged read-only tasks lightweight.

To publish a verified substantial task, run `cw publish` under the user's
authorization; for its fallback cases, or when the user asks for a bounded
worker, dispatch the [GitHub worker](github-worker.md); a small task commits and
pushes directly under the user's authorization. The worker drafts messages and
PR text from fixed verified facts and performs scoped, authorized commit/push and
`gh` PR writes with refresh and readback. Its three roles use the `github` profile;
substantive `pr_authoring` stays with the substantive profile. New claims or changed
scope return to the coordinator. Publishing already verified work alone does not
require a new substantial task or repeat adviser checkpoints. Commit, push, merge,
deployment and infrastructure actions retain their own authorization.

## Planning

Separate source tracing, acceptance/dependency discovery, and failure-behavior
analysis when independent. Reconcile the smallest complete plan: owners,
interfaces, failure behavior, verification, rollout/recovery, and material choices.
Track assumptions with basis, status, impact, and next check. A plan remains a plan
until implementation is authorized, even when referenced issues contain detailed
implementation instructions.

## Review

Pin base/head or the working diff, including relevant untracked files. Allocate
distinct correctness, lifecycle, security, compatibility, or test questions when
useful. Trace candidates through definitions and callers; return exact lines,
trigger, impact, evidence, and whether the change introduces or exposes the defect.
Reconcile overlaps and current review threads before authorized publication.
Refresh affected findings when the head changes. Honor read-only/no-local-test
scope and separate static, existing CI, local, and deployed evidence.

## Diagnosis

Separate runtime observations, source paths, and deployment/configuration facts.
Retain timestamps, versions, environment, supported hypotheses, contradictory
evidence, and the next discriminating check. Plausible source behavior does not
establish incident cause. Rebuild affected hypotheses after new facts; preserve
user choices and still-valid evidence. Complete the authorized diagnosis or fix,
separating observed cause, inference, reproduction, and uncertainty. Operational
mutations require their own scope.

## Release verification

Pin the intended SHA/tag, workflow, required checks, artifact identity, and release
scope. Divide independent CI, artifact, and compatibility questions without
repeating checks. Tie results to source, run, inputs, and target environment.
Pending or skipped required gates remain unverified. A tag is not a completed
release; green CI is not deployment or production qualification. Respect CI-only
and no-infrastructure-access restrictions. Report completed, failed, and pending
gates and the concrete permitted next action; refresh affected claims after new
runs or artifacts.

## Documentation

Agree audience, terminology, authority, and section ownership. Verify source before
substantive drafting; label proposals and operational claims by their evidence.
Reconcile drafts and verify links or rendered output where presentation matters.
Save/publish only to the authorized destination; otherwise return the draft.
For Confluence revisions and formatting, read the connected-source guide.

## Issue management

Separate bounded lookup from substantive triage, acceptance analysis, and issue
text. Search duplicates and dependencies; use current project/type fields and
actual acceptance criteria. Preserve uncertainty and prepare coherent requested
edits, comments, or transitions. A referenced issue is not authority to mutate it.
Reconcile uncertain writes through readback. Status, merge, or green CI alone does
not satisfy acceptance or production readiness. Close only within the requested
scope and after its criteria are met, using current available transitions.

## Mixed tasks and refreshed evidence

Combine relevant sections while keeping each deliverable's acceptance, scope, and
authorization visible. Refresh only claims and assignments affected by changed
sources, docs, CI, environment, failures, or scope. New context is not new permission.
