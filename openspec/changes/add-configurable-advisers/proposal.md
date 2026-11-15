# Proposal: add-configurable-advisers

Status: implementation and automated validation completed on 15 November 2026. Formal stage
reviews and human approvals have not been recorded. No live adviser call, commit, or publication
is claimed by this record.

## Why

The owner wants configured advisers that any permitted workflow role can ask for an opinion or a
choice between alternatives. The coordinator needs a durable answer and a reasoned disposition,
with optional required advice at specific stages. A table should make each role's model,
reasoning effort, and adviser access visible on both supported hosts.

## What changes

- Optional personal adviser definitions at `os.UserConfigDir()/kitsu/advisers.toml` and repository
  overrides in `kitsu-advisers.toml`. Only the repository sets access and required checkpoints.
- A command array and requested model for each adviser, executed once through an existing
  authenticated CLI. Advice and decisions use a bounded version 1 JSON request and explicit files.
- Saved task bindings, immutable consultation attempts, appended dispositions, and offline checks.
  Full and small workflows keep their own portable records; promotion moves the whole history.
- `kitsu advisers list`, `bind`, `resolve`, `status`, and `check`, plus root `kitsu consult`.
- Coordinator instructions to broker requests after checking the requesting role's writes, retain
  independent review, and satisfy required checkpoints before leaving their stages.
- `kitsu roles --host claude|codex [--task <task>]` for a role/model/reasoning/advisers table, and
  differentiated specialist defaults for each host. The coordinator remains the host session.
- Adviser usage reported separately as unmeasured; configured models are not verified actual models.

## Capabilities

`configurable-advisers` adds configuration, calls, bindings, history, checkpoint checks, workflow
handoffs, usage disclosure, and role selection reporting. The historical role-workflow proposal's
exclusion of adviser CLI calls is superseded by this change; the gate still makes no network calls.

## Sources and accepted decisions

The owner's requests in this conversation authorize configurable advisers, any permitted role as
a requester, advice and decisions, required checkpoints, and a role/model/reasoning table on both
hosts. The accepted implementation plan selects personal definitions plus repository policy,
coordinator brokering, local CLI authentication, portable history, explicit refresh, and no
automatic retry or model fallback. The owner's later correction selects Sol at max for the Codex
architect. The model route is written in the [design](design.md#model-routing).

Technical references are the existing `internal/roles`, `internal/change`, `internal/proc`,
`internal/gitx`, and `internal/tokens` interfaces. Provider CLI examples were checked against local
help, not a live model call. Claude Code's supported effort levels were checked in its
[official model configuration documentation](https://code.claude.com/docs/en/model-config#adjust-effort-level).
Codex model names were checked against the local host model catalog. These facts establish
configuration choices, not a benchmark of the models or evidence of what a provider actually ran.

## Not part of this change

Provider API clients, stored API keys, automatic retries or escalation, a model router, budgets or
prices, parallel adviser calls, and an interactive model picker. Advice cannot waive serious
review findings, replace a required review, or supply the developer's approval.

## Validation and limits

Tests use local fake executables and temporary repositories. They must cover valid and invalid
configuration, access, outcomes, history, freshness, promotion, and offline checks without a
provider account. Generated role files and prompt budgets remain checked. The existing build,
vet, race tests, lint, vulnerability checks, and change-record checks remain the quality gates.
Live model quality, cost, actual model identity, and successful host behavior remain unmeasured.

## Open questions

None for the accepted implementation scope. Model availability depends on the developer's host
and account; a missing model is reported rather than substituted.
