# Proposal: add-agent-onboarding

Status: implementation and automated validation completed on 16 November 2026. Targeted
independent review found a disabled-Codex-plugin readiness bug; its fix was reproduced and
rechecked. This record supplies no formal stage review or human approval and claims no live
model run or release publication.

## Why

Installing the CLI does not show whether the selected host can load Kitsu's task skill or run
its hooks. A new session also needs a clear way to start coordinated work before a repository
has configured the supporting Git gate.

## What changes

- `kitsu agent check` inspects local CLI, selected host, plugin, task skill, and hook configuration,
  reports demonstrated problems separately from unknown trust or runtime behavior, and gives next steps.
- `kitsu task start <request>` prepares a task-skill handoff and explicitly launches the selected
  interactive host after local preflight. `--print` and `--json` prepare the handoff without execution.
- Session-start context introduces the coordinator workflow in Git repositories with or without
  `kitsu.toml`, while preserving concise and accurate gate evidence instructions where configured.
  This replaces the earlier `agent-integration` Session context rule that repositories without
  a declaration received no context. The workflow introduction now precedes the first gate
  paragraph; that paragraph still states missing hooks before any enforcement claim.
- A session-start hook that cannot find the CLI gives install/PATH guidance without requiring
  a declaration, Git, or a repository root. It no longer claims all commits and pushes are denied.
- Help, completion, generated command reference, installation guidance, and practical onboarding docs
  explain what was checked, how to recover, and what still needs a host session.

## Capability

`agent-onboarding` adds local diagnostics and a host handoff. The host continues to execute and
delegate work. Task records, independent reviews, human approvals, and content-bound gate evidence
retain their existing meaning.

## Sources and decisions

The owner requested implementation and verification of the CLI/plugin/hook onboarding path and
a clear task-start command, using parallel Sol workers. That authorizes implementation work,
not fabricated approval or review entries.

Host guidance was checked on 16 November 2026 against the official
[Codex plugin guide](https://learn.chatgpt.com/docs/plugins),
[Codex hook guide](https://learn.chatgpt.com/docs/hooks),
[Claude Code plugin guide](https://code.claude.com/docs/en/discover-plugins), and
[Claude Code CLI reference](https://code.claude.com/docs/en/cli-reference).
Local configuration can establish installation and explicit disable settings; it cannot prove
that an interactive session trusts the current hooks or executed them.

## Scope limits

Diagnostics and handoff preparation do not install software, edit host settings, accept trust,
authenticate, invoke models, initialize the Git gate, or create workflow records. The explicit
default task-start action invokes an interactive host and can begin model work there. It adds
no scheduler and does not claim a completed workflow stage from starting that process.

## Validation

Tests cover isolated host layouts, missing and disabled components, unknown runtime evidence,
host precedence, safe help, handoff-only modes, terminal/preflight refusal, literal process
arguments, session context, and generated documentation. `tests.md` records actual mappings,
observed command results, and limits. Full Linux race tests and three-target lint/vulnerability
checks passed; Darwin and Windows test binaries cross-compiled, with no native runtime claim.
