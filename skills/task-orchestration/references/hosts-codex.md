# Codex host

Read with the shared [host mapping](hosts.md).

## Profiles

Codex reads profiles and roles from [model-policy.yaml](../model-policy.yaml).
Defaults: `sol` at `high` for substantive work and
`decision` at `max` for planning, architecture and diagnosis; `luna` at `high`
for bounded evidence and `github`, and `luna_execution` at `xhigh` for other
bounded execution.

## Worker isolation

Codex has no per-agent switch to keep instruction files out of a worker: it
injects `$CODEX_HOME/AGENTS.md` and the project `AGENTS.md` into a spawned
agent even with `fork_turns: "none"`, as reported for Codex CLI 0.166.0 in
[google/elephant-goldfish-model#237](https://github.com/google/elephant-goldfish-model/issues/237)
(open), so a Codex reviewer or test engineer sees those files. Keep task
conclusions out of `AGENTS.md`, and report the independence of a Codex
reviewer or test engineer as limited by them.

## Independent test engineers

Create a distinct worker with `fork_turns: "none"` and explicit `model`
and `reasoning_effort`.
