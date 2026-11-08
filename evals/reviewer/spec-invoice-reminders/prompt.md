---
description: Spec review of an OpenSpec change with an untestable scenario and a requirement that contradicts the current code.
tags: [reviewer, spec-review]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write, Edit, Bash]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `reviewer` agent exactly once
through the Agent tool (`subagent_type: "agent-workflows:reviewer"`) with the
brief below, wait for it, then reply with the review path and its finding
counts by severity, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must name the `spec-review` contract and tell the agent
to read it from its default contract directory (the agent's own definition
names the path), give the absolute path of the current working directory, and
say that every path below is relative to it: inputs under `testdata/` and the
artefacts under `artefacts/`. Otherwise pass the brief verbatim.

Brief for the reviewer:
- Review contract: `spec-review`.
- Goal: review the change `add-invoice-reminders` before implementation.
- Change folder: `testdata/billing/openspec/changes/add-invoice-reminders/`.
- Source documents (the same the writer had): `testdata/sources/BILL-51.md`.
- Repository: `testdata/billing`.
- Artefact directory: `artefacts/`. Owned paths: `artefacts/spec-review.md`
  and `artefacts/handoff.json` (the handoff result JSON; this run has no `cw`
  task record, so leave out the task, assignment, agent and snapshot fields).
- Stop condition: every requirement, scenario and task has been read and
  `artefacts/spec-review.md` is written.
