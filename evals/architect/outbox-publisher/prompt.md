---
description: Design reliable event publication for an order service that currently publishes after commit.
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
- Request: make `OrderPlaced` publication in `testdata/orders` reliable. Every
  committed order must be published at least once, events for one order must
  reach Kafka in commit order, and a crash between commit and publish must
  not lose the event.
- Facts: orders are stored in Postgres through `Service.Place`; today the
  event is published in a goroutine after the transaction commits. Kafka and
  Postgres are already deployed. Consumers can be changed in a later release.
- Decide: the component boundaries (what writes, what relays, who owns
  ordering and deduplication), failure behaviour of each piece, and how to
  roll the change out without a dual-write window and back it out.
- Out of scope: exactly-once delivery, consumer rewrites, schema registry.
- Repository evidence: cite `path:line` from `testdata/orders`.
