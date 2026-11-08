---
description: Code review of a webhook dispatcher with an untested 410 requirement, an unrequested fallback signing secret and a sender that bypasses the injected HTTP client.
tags: [reviewer, code-review]
runs: 1
max_turns: 60
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write, Edit, Bash]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `reviewer` agent exactly once
through the Agent tool (`subagent_type: "agent-workflows:reviewer"`) with the
brief below, wait for it, then reply with the review path and its finding
counts by severity, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must name the `code-review` contract and tell the
agent to read it from its default contract directory (the agent's own
definition names the path), give the absolute path of the current working
directory, and say that every path below is relative to it: inputs under
`testdata/` and the artefacts under `artefacts/`. Otherwise pass the brief
verbatim.

Brief for the reviewer:
- Review contract: `code-review`.
- Goal: final code review of the change `add-webhook-dispatch`, which adds
  Go package `dispatch` in module `example.com/hooks`, against requirements
  R1 to R6.
- Interface contract: `testdata/design/interface-contract.md`. Decision
  record: `testdata/design/design.md`.
- Integrated source: `testdata/hooks`. Every file under
  `testdata/hooks/dispatch/` is new in this change, so every line in it is a
  changed line; `dispatch_test.go` is the independent test engineer's suite.
- Test engineer's matrix and requirement map: not part of this run; each
  test's comment names the requirement it checks.
- Final check run: `testdata/checks/unit.log` (passed). Its coverage
  profile: `testdata/checks/cover.out`; its file paths start with the module
  path `example.com/hooks`.
- Experiments: work only in the scratch copy of the integrated source,
  `artefacts/scratch/`, which is already in place; it is a Go module, so
  run `go test` there.
- Artefact directory: `artefacts/`. Owned paths: `artefacts/code-review.md`
  (the findings, with IDs `F1`, `F2`, ..., and everything the contract asks
  each one to carry, then the inputs read and what was not reviewed),
  `artefacts/handoff.json` (the handoff result JSON; this run has no `cw`
  task record and no bundle, so leave out the task, assignment, agent and
  snapshot fields and the reach runs) and any scratch files under
  `artefacts/scratch/`.
- Stop condition: every changed file has been read, every unexecuted changed
  block in the profile is classified and `artefacts/code-review.md` is
  written.
