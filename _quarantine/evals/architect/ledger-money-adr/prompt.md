---
description: Design an exact money representation for a ledger whose repository keeps ADRs in docs/decisions/; the costly-to-reverse choice gets an ADR in the decision-record format.
tags: [architect, adr]
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
under `testdata/`, the Markdown artefacts under `artefacts/`, and repository
files exactly at their owned paths. Otherwise pass the brief verbatim.

Brief for the architect:
- Request: make ledger amounts exact, as `testdata/sources/LEDG-12.md` asks.
  Decide how money is represented in the `entries` table, in Go and in the
  balance API, how existing rows move to it, and how to roll it out and back.
- Facts: about 40 million rows (`testdata/ledger/schema.sql`); the mobile app
  and the partner reporting export parse `balance` as a JSON number and can
  switch to a new field in a later release.
- Out of scope: currency conversion, rounding rules for interest.
- Repository: `testdata/ledger`, which keeps its ADRs in
  `testdata/ledger/docs/decisions/`. Cite `path:line` from it.
- Owned paths: `artefacts/design.md`, `artefacts/interface-contract.md`,
  `artefacts/implementation-brief.md`, and
  `testdata/ledger/docs/decisions/0002-money-representation.md` if your
  contract calls for an ADR.
