# Proposal: add-compact-context

Status: implementation requested by the owner; local verification is recorded in tests.md.
No paid model comparison, native cross-platform runtime test or human stage approval is claimed.

## Why

Repeated repository observations, separate worker-brief preparation commands and noisy check
logs can consume the coordinator's context. The owner asked to borrow useful batching and
filtering ideas from Code Mode using a built-in Go engine, then approved exploration, handoffs,
declared checks, offline validation and an opt-in rollout. The owner also requested subagent
research into total token use and workflow efficiency.

## What changes

- Add versioned, non-executable context collect/show commands with exact byte limits and
  expandable private worktree artifacts, preserving bare context session-hook behavior.
- Prepare factual worker briefs with current role/model/reasoning, skills, write constraints,
  allowed advisers and task state, recording skill selection before the worker baseline.
- Run declared checks through their existing sequential runner while returning compact JSON
  outcomes and bounded diagnostics; retain ordinary logs and evidence separately.
- Opt in through task start --compact or an explicit request to the installed task skill.
- Supply regressions, offline comparisons, primary-source research, practical docs and diagrams.

## Scope limits

No generated code execution, arbitrary shell-operation API, host-runtime replacement, model
router, paid-model acceptance run, autonomous approval, source editing, stage skipping or new
tool grant is introduced. Context artifacts never count as passing gate evidence or skill
loading. Default task behavior remains unchanged. Fewer returned bytes are not measured model
tokens, and local cache hits are not provider prompt-cache hits.
