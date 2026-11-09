---
description: Adversarial testing of a length-prefixed frame decoder whose black-box tests use lengths near the limit only; probes must reach the full uint64 length range.
tags: [adversarial-tester, parsing]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write, Edit, Bash]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `adversarial-tester` agent
exactly once through the Agent tool
(`subagent_type: "agent-workflows:adversarial-tester"`) with the brief below,
wait for it, then reply with the findings path and its finding counts by
severity, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must tell the agent to read its contract from its
default contract directory (the agent's own definition names the path), give
the absolute path of the current working directory, and say that every path
below is relative to it: inputs under `testdata/` and the artefacts under
`artefacts/`. Otherwise pass the brief verbatim.

Brief for the adversarial tester:
- Stage: adversarial testing of the integrated change. Opted-in area:
  parsing (untrusted input).
- Goal: try to break Go package `frame` against requirements R1 to R5 of the
  interface contract and design rules DR1 to DR3 of the design.
- Interface contract: `testdata/contract/interface-contract.md`; design:
  `testdata/contract/design.md`. Both are read-only.
- Integrated source and its black-box tests: `testdata/work/` (module
  `example.com/frame`, package `frame`), read-only. Build and test command:
  `go test ./...` in `testdata/work`.
- Coverage profile of the final check run: `testdata/coverage/cover.out`.
- Scratch copy for probes: `artefacts/scratch/` already holds a copy of
  `testdata/work` with its `go.mod`; run every probe there, never in
  `testdata/work`.
- Artefact directory: `artefacts/`. Owned paths:
  `artefacts/adversarial-findings.md`, `artefacts/adversarial_test.go` (every
  kept test in one file, which belongs at `testdata/work/adversarial_test.go`
  in package `frame`, so unexported names are reachable),
  `artefacts/handoff.json` (the handoff result JSON; this run has no `cw` task
  record, so leave out the task, assignment, agent and snapshot fields) and the
  scratch copy under `artefacts/scratch/`.
- Stop condition: every requirement and design rule in the opted-in area has
  been probed or listed as not probed; return the findings, the discarded
  probes and what was not probed.
