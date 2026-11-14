---
description: Design the service boundary for a per-tenant rate limiter shared across gateway replicas.
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
- Request: add per-tenant rate limiting to the API gateway in `testdata/gateway`.
  Each tenant gets 100 requests per second with a burst of 200; tenants come
  from the existing `config.yaml`, and the limits must be added there.
  Over-limit requests get HTTP 429 with a `Retry-After` header.
- Facts: the gateway runs as three identical replicas behind a load balancer
  and a tenant's requests can reach any replica. One Redis instance is already
  deployed for sessions. The tenant is identified by `tenantFromRequest` in
  `middleware.go`.
- Decide: where the limiter lives, how replicas share counts, what happens
  when Redis is slow or unreachable, and how the change rolls out and backs
  out across the three replicas.
- Out of scope: billing, per-endpoint limits, UI.
- Repository evidence: cite `path:line` from `testdata/gateway`.
