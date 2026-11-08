---
description: Turn a product brief into an OpenSpec change for a repository that has no openspec directory yet.
tags: [spec-writer]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write, Edit, Bash]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `spec-writer` agent exactly once
through the Agent tool (`subagent_type: "agent-workflows:spec-writer"`) with the
brief below, wait for it, then reply with the paths it wrote and its open
questions, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must tell the agent to read its default contract (the
agent's own definition names the path), give the absolute path of the current
working directory, and say that every path below is relative to it: inputs
under `testdata/`, the artefacts under `artefacts/`, and the change folder
exactly at the path given below. Otherwise pass the brief verbatim.

Brief for the spec writer:
- Goal: write the OpenSpec change for exporting bookmarks as CSV.
- Source documents: `testdata/product-brief.md`.
- Repository: `testdata/bookmarks`.
- Change ID: `add-bookmark-export`. Capability: `bookmark-export` (new).
- Change folder: `testdata/bookmarks/openspec/changes/add-bookmark-export/`.
- Artefact directory: `artefacts/`. Write the handoff result JSON to
  `artefacts/handoff.json`; this run has no `cw` task record, so leave out the
  task, assignment, agent and snapshot fields.
- Stop condition: every listed capability has its delta, every requirement a
  scenario and every task a check.
