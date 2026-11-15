# Proposal: improve-cli-ux

Status: implementation and automated validation completed on 15 November 2026. Targeted
independent review findings were fixed and rechecked. No complete-stage review, human approval,
live provider call, commit, or publication is claimed by this record.

## Why

The owner wants commands that are easy to discover, consistent help at every level, a saved
host preference, explicit terminal editors, and shell completion. Existing scripts and host
integrations must keep working while documentation leads with a coherent command tree.

## What changes

- Preferred singular `task`, `role`, `skill`, `adviser`, `check`, `gate`, `usage`, and `config`
  groups, four shell completion commands, and retained version and advanced integration paths.
- Complete offline help before setup, successful no-argument root/group help, concise leaf
  usage errors, and nonexecuting typo suggestions. Existing aliases stay quiet and retain their
  bare-command behavior.
- One metadata registry for dispatch, help, examples, and completion. New inspect/list actions
  expose existing configuration and full or small records without inventing a small-task stage.
- Private per-worktree and personal host preferences, with flag and environment precedence.
- Explicit Huh v2 role/config editors with field help, accessible mode, preview, save-time
  validation, cancellation without writes, and concurrent-edit protection.
- Dedicated skill attach, detach, clear, and reset operations with distinct inheritance behavior.
- README, how-to, references, role command instructions, and properly regenerated archify
  diagrams that use preferred command names.

## Capabilities

`cli-ux` adds discovery and editing affordances around existing role, skill, adviser, task,
gate, and usage behavior. Existing aliases, serialized task records, role scopes, experimental
adviser opt-in, review independence, human approvals, and evidence contracts keep their authority.

## Sources and accepted decisions

The owner's accepted CLI UX plan authorizes implementation of the command surface, compatibility,
help rules, precedence, Huh v2 editors, completion, documentation, and diagram updates. The
owner requested Sol at max for parallel implementation. Parallel work and automated checks do
not supply independent workflow stage reviews or human approvals.

Technical sources are existing `internal/cli` command handlers, `internal/roles` editing and
scope guards, `internal/skills` original-source resolution, `internal/change` record checking,
adviser history, and the repository's generated role and documentation checks.

## Not part of this change

A new agent runtime, automatic command correction or execution, implicit setup or interactive
fallback, new adviser access, model availability probes, changed permission or approval rules,
provider requests from help/completion, or claims of live host loading and compliance.

## Validation and limits

Tests cover the preferred tree and aliases, every help ingress, offline and malformed
configuration, missing operands, leaf errors, typo suggestions, host precedence and isolation,
explicit editor cancellation and saving, skill inheritance, shell scripts and callbacks,
inspection, unknown small stages, guard parity, and generated files. Diagram validation includes
archify finalization, actual canonical SVG exports, and light/dark visual inspection. Actual
commands, completed results, and runtime limits are recorded in `tests.md`.

## Open questions

None for the accepted scope. Native shell runtime availability and actual terminal interaction
are reported with their observed validation limits rather than inferred from generated files.
