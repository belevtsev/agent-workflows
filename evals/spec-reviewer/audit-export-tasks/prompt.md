---
description: Review an OpenSpec change with unverifiable tasks and format errors, without the writer's notes.
tags: [spec-reviewer]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write, Edit, Bash]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `spec-reviewer` agent exactly
once through the Agent tool (`subagent_type: "agent-workflows:spec-reviewer"`)
with the brief below, wait for it, then reply with the review path and its
finding counts by severity, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must tell the agent to read its default contract (the
agent's own definition names the path), give the absolute path of the current
working directory, and say that every path below is relative to it: inputs
under `testdata/` and the artefacts under `artefacts/`. Otherwise pass the
brief verbatim.

Brief for the spec reviewer (pass it only the change folder, the source
document and the repository below; never mention or pass
`testdata/writer-notes.md`, which holds the spec writer's notes):
- Goal: review the change `add-audit-log-export` before implementation.
- Change folder: `testdata/audit/openspec/changes/add-audit-log-export/`.
- Source documents (the same the writer had): `testdata/sources/SEC-77.md`.
- Repository: `testdata/audit`.
- Artefact directory: `artefacts/`. Owned paths: `artefacts/spec-review.md`
  and `artefacts/handoff.json` (the handoff result JSON; this run has no `cw`
  task record, so leave out the task, assignment, agent and snapshot fields).
- Stop condition: every requirement, scenario and task has been read and
  `artefacts/spec-review.md` is written.
