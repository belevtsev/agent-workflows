---
description: Design review of a field rename that breaks pinned clients, has no compatible rollout and drops the spec's conflicting-values behaviour.
tags: [reviewer, design-review]
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
named below. The brief must name the `design-review` contract and tell the
agent to read it from its default contract directory (the agent's own
definition names the path), give the absolute path of the current working
directory, and say that every path below is relative to it: inputs under
`testdata/` and the artefacts under `artefacts/`. Otherwise pass the brief
verbatim.

Brief for the reviewer (pass it only the design artefacts, the reviewed
change folder, the source document and the repository below; never mention
or pass `testdata/architect-transcript.md`, which holds the architect's
conversation):
- Review contract: `design-review`.
- Goal: review the architect's design for the change `add-timeout-ms`
  before the contract is fixed and implementation starts.
- Decision record: `testdata/design/design.md`.
- Interface contract: `testdata/design/interface-contract.md`.
- Reviewed change folder: `testdata/jobs/openspec/changes/add-timeout-ms/`.
- Source documents: `testdata/sources/API-412.md`.
- Repository: `testdata/jobs`, including the Go SDK in `testdata/jobs/client`.
- Artefact directory: `artefacts/`. Owned paths: `artefacts/design-review.md`
  and `artefacts/handoff.json` (the handoff result JSON; this run has no `cw`
  task record, so leave out the task, assignment, agent and snapshot fields).
- Stop condition: every requirement, section and option has been read and
  `artefacts/design-review.md` is written.
