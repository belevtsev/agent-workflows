# Proposal: add-existing-skills

Status: implementation and automated validation completed on 15 November 2026, including
integration with native advisers on current main. A targeted independent receipt review confirmed
its two findings fixed. Complete-stage reviews, human approvals, and live host loading are not
claimed by this record.

## Why

The owner wants Kitsu roles to use skills already present in repositories, personal directories,
and installed plugins. Copying skill bodies would separate instructions from their scripts and
references. Role selection needs a local inventory, explicit bindings, and an inspectable source
handoff while keeping the host responsible for running workers.

## What changes

- Read-only Codex and Claude Code inventories with qualified ids, source diagnostics, metadata
  validation, observable host disable policy, and explicit roots for unsupported local layouts.
- Optional per-host, per-role skill arrays in repository `kitsu-roles.toml` and personal
  `os.UserConfigDir()/kitsu/roles.toml`. Repository arrays replace personal lists; `[]` suppresses
  inheritance. No skills are pinned by default.
- `kitsu skills list`, `kitsu roles set --skills`, personal `--global` skill bindings, and skill
  selections alongside model, reasoning, and advisers in the role table and JSON.
- `kitsu skills resolve` for current original paths and hashes, with optional portable immutable
  full/small task receipts, offline record checks, and small-to-full history preservation.
- Fresh-worker brief instructions to load the selected originals, plus Claude Code's `Skill`
  tool. Kitsu's generic role texts remain independent of language and build-tool expertise.
- A practical [skill guide](../../../docs/skills.md), README examples, and command references.

## Capabilities

`existing-skills` adds discovery, selection, source handoffs, portable receipts, and reporting.
Existing role write limits, review independence, human approval rules, model settings, adviser
policies, and gate evidence keep their own authority. The coordinator can select skills while
its model and reasoning remain host-session settings.

## Sources and accepted decisions

The owner's requests authorize implementation of existing-skill discovery and per-role
selection, rather than another language-skill distribution. The accepted plan uses original
local skill directories, explicit repository or personal bindings, and a new-worker handoff.
The owner requested parallel implementation work with Sol at max. That implementation activity
does not constitute an independent spec, design, or code review in the workflow ledger.

Technical sources are the existing `internal/roles` configuration and editing code, generated
Claude role files, coordinator texts, `internal/cli` command parsing, and change-record checker.
Local catalogs establish only observable files and configuration. No live Codex or Claude worker
run is evidence for this change.

## Not part of this change

Installing, downloading, copying, or executing discovered skills; invoking hosts or model
processes during discovery; creating a skill marketplace; new language packs; a new agent
runtime; automatic skill choice from task prose; or guarantees of host catalog completeness,
loading, or compliance. A selection receipt is not loading evidence.

## Validation and limits

Tests use temporary local catalogs, settings, plugins, repositories, and receipts without host
or model execution. They cover metadata, conflicting names, observable disable settings,
configuration layering, clear/reset behavior, original paths and hashes, portability, immutable
history, promotion, offline checks, generated files, and retained role limits. Actual commands
and results belong in `tests.md`; no check is claimed until its result is recorded there.

## Open questions

None for the accepted scope. Unsupported or opaque host catalog layouts require explicit local
roots; the inventory reports its limits instead of claiming the host's complete effective view.
