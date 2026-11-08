---
description: Test review of a rate limiter's matrix and tests with an uncovered requirement, a mirror assertion and a sleep-synchronised concurrency test.
tags: [reviewer, test-review]
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
named below. The brief must name the `test-review` contract and tell the
agent to read it from its default contract directory (the agent's own
definition names the path), give the absolute path of the current working
directory, and say that every path below is relative to it: inputs under
`testdata/` and the artefacts under `artefacts/`. Otherwise pass the brief
verbatim.

Brief for the reviewer (pass it only the contract, the test engineer's
artefacts and the stub below; never mention or pass `testdata/impl`, which
holds the implementation):
- Review contract: `test-review`.
- Goal: review the test engineer's matrix and tests for Go package
  `ratelimit` against requirements R1 to R6 before integration.
- Interface contract: `testdata/contract/interface-contract.md`; declaration
  extract: `testdata/contract/declarations.go`.
- Test engineer's artefacts: `testdata/tests/test-matrix.md`,
  `testdata/tests/requirement-map.md` and the tests
  `testdata/work/limiter_test.go`.
- Red-first runs: `testdata/work/limiter.go` is a stub built from the
  declaration extract that returns zero values, in module
  `example.com/ratelimit`; run `go test ./...` in `testdata/work` against it,
  and add mutants of the stub only under `artefacts/`.
- Artefact directory: `artefacts/`. Owned paths: `artefacts/test-review.md`,
  `artefacts/handoff.json` (the handoff result JSON; this run has no `cw`
  task record, so leave out the task, assignment, agent and snapshot fields)
  and any scratch files under `artefacts/`.
- Stop condition: every requirement, matrix row and test has been checked
  and `artefacts/test-review.md` is written.
