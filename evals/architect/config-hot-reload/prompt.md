---
description: Design configuration reload on SIGHUP for a daemon without dropping in-flight work.
tags: [architect]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `architect` agent exactly once
through the Agent tool (`subagent_type: "agent-workflows:architect"`) with the
brief below, wait for it, then reply with the paths it wrote and its open
questions, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must tell the agent to read its default contract (the
agent's own definition names the path), give the absolute path of the current
working directory, and say that every path below is relative to it: inputs
under `testdata/`, the Markdown artefacts under `artefacts/`, and code or test
files exactly at their owned paths. Otherwise pass the brief verbatim.

Brief for the architect:
- Request: let `ingestd` in `testdata/ingestd` reload its configuration on
  SIGHUP. In-flight batches must finish under the configuration they started
  with; an invalid new file must leave the old configuration active and be
  reported; the active configuration's version must be visible on `/healthz`.
- Facts: configuration is loaded once in `main` and passed by pointer to the
  batch loop and the HTTP handler. Batches take up to 30 seconds. One process
  per host; no orchestrator restarts it on signal.
- Decide: who owns the active configuration, how readers get a consistent
  snapshot, what happens when a reload arrives during a batch or during a
  previous reload, and how the change rolls out and backs out.
- Out of scope: a new configuration format, remote configuration, metrics.
- Repository evidence: cite `path:line` from `testdata/ingestd`.
