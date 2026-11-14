---
description: Write the test matrix, tests and requirement map for a bounded TTL cache from its contract.
tags: [test-engineer]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `test-engineer` agent exactly once
through the Agent tool (`subagent_type: "agent-workflows:test-engineer"`) with the
brief below, wait for it, then reply with the paths it wrote and its open
questions, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must tell the agent to read its default contract (the
agent's own definition names the path), give the absolute path of the current
working directory, and say that every path below is relative to it: inputs
under `testdata/`, the Markdown artefacts under `artefacts/`, and code or test
files exactly at their owned paths. Otherwise pass the brief verbatim.

Brief for the test engineer (pass it only `testdata/contract/interface-contract.md`
and `testdata/contract/declarations.go`; never mention or pass
`testdata/impl`, which holds the implementation):
- Goal: the test matrix, tests and requirement map for Go package `cache`
  (module `example.com/cache`) against requirements R1 to R8.
- Owned paths: `cache/cache_test.go` (external test package `cache_test`),
  `artefacts/test-matrix.md`, `artefacts/requirement-map.md`.
- Approved inputs: the interface contract and the declaration extract. Apply
  the `test-strategy` skill's references on test design and property testing:
  `../../../test-strategy/references/test-design.md` and
  `../../../test-strategy/references/property-testing.md`, relative to your
  contract's directory.
- Scope: unit and property tests; the clock is injected, so no real time.
  Include a model-based property test of the eviction order.
- Stop condition: every requirement is covered or marked `uncovered` with a
  reason. You do not run the implementation.
