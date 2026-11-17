# Design: add-agent-onboarding

Read local evidence once, present it with its limits, and hand the accepted task to the chosen
host. Execution remains in the host's interactive session.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Local diagnostics | Inspect executable paths, installed/enabled Kitsu plugin, original task skill, hook bundle, and known disable settings; return evidence and next steps | none | yes | Existing skill discovery and host config readers inform supported layouts; do not infer runtime from a skill catalog entry |
| Command adapters | Resolve host preferences, expose text/JSON diagnostics, and register help and completion | Local diagnostics | yes | Existing preferred-command metadata, host precedence, usage errors, and readonly CLI conventions |
| Task handoff | Prepare an explicit task-skill prompt and host argv; print it or launch the interactive host after terminal and local preflight checks | Local diagnostics | yes | Existing task skill and role contracts; retain host execution instead of adding another workflow engine |
| Session context | Introduce the task workflow before gate facts; give safe missing-CLI hook guidance | none | no | Existing context command, hook cwd parsing, absolute PATH filtering, time limits, and 1,000-character context budget |
| Documentation | Explain install/check/start, evidence limits, recovery, and handoff-only use; regenerate command reference | Command adapters, Task handoff, Session context | no | Existing README, CLI/operations references, role docs, and generated-reference check |

## Evidence and preflight

The diagnostic report retains per-component status, evidence, and next steps. A discovered CLI
path proves lookup, not authentication or model access. An installed plugin, enabled setting,
task file, and configured hook bundle establish local setup only. Missing or explicitly
disabled components are actionable problems. Inconclusive required installation inventory
prevents launch while stating what could not be established rather than claiming actual absence.
Codex hook trust and actual session hook execution remain
unknown without direct evidence. The CLI neither reads host credentials nor runs a host to probe them.

The Git gate is a separate supporting feature. Absence of kitsu.toml, Git hooks, or passing check
evidence is not a coordinator onboarding prerequisite. Existing gate status and check commands
remain the place to inspect them.

## Handoff and execution

Host selection uses explicit --host, nonempty KITSU_HOST, private worktree preference, then
personal preference. Both supported hosts receive an instruction to load Kitsu's task skill
and the user's literal request. Claude Code uses /kitsu:task; Codex uses the installed Kitsu task
skill. Host invocation uses an argv list without a shell, retaining the current directory and
normal host permission, authentication, and model choices.

Default task start requires an interactive terminal and acceptable local preflight. It explains
remaining unknown trust/runtime facts and explicitly invokes codex or claude. Handoff-only
--print and --json skip launch and remain useful when host prerequisites are missing. They
return both a prompt and diagnostics without creating a task folder, receipt, stage, or usage
record. Help runs before configuration, operand, terminal, or diagnostic checks.

## Context and generated content

Session-start context first names Kitsu's orchestration workflow and how to invoke the task
skill. It does not start a workflow automatically. A Git repository without kitsu.toml still
gets the introduction. Gate facts are appended only when declared and distinguish installation,
observe mode, and evidence production. Context stays within the existing budget. Generated
host prompts are rebuilt from their source; command help and reference come from metadata.
The existing diagram topology remains valid and needs no change.

When the installed CLI is unavailable, SessionStart prints only install/PATH guidance and
claims no gate enforcement. The fallback works without Git or kitsu.toml and from subfolders;
POSIX hooks still execute no repository binary and remain quiet if PATH has no usable absolute
entry. Windows retains its safe lookup and fallback on executable-not-found status. The hook
does not carry an inline copy of the orchestration prompt.

## Rules

| Rule | Checked by |
| --- | --- |
| Diagnostics inspect only local setup, preserve evidence limits, and give actionable recovery | test |
| Demonstrated setup failure and unknown host trust/runtime remain distinct | test |
| Checks, help, print, and JSON do not execute a host, model, hook, install, or config write | test |
| Default launch requires a terminal and local preflight and passes literal task text without a shell | test |
| Host selection follows existing precedence and invalid usage receives specific help | test |
| Session context introduces orchestration without gate prerequisites or automatic workflow start | test |
| Missing-CLI guidance reaches undeclared directories without executing unsafe PATH entries or claiming enforcement | test |
| Generated reference, documentation links, task mappings, and existing budgets stay checked | test |
| Host instructions preserve role writes, reviews, human approvals, and evidence authority | review |
