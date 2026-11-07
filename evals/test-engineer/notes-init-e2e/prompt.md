---
description: Write end-to-end tests for a CLI init command from its contract, running the built binary.
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

Brief for the test engineer (pass it only `testdata/contract/interface-contract.md`;
never mention or pass `testdata/impl`, which holds the implementation):
- Goal: the test matrix, end-to-end tests and requirement map for the `notes`
  command-line tool against requirements R1 to R7.
- Owned paths: `e2e/init_test.go` (package `e2e`), `artefacts/test-matrix.md`,
  `artefacts/requirement-map.md`.
- Approved inputs: the interface contract. Apply the end-to-end reference
  your contract links. The binary is built by `go build -o <tmp>/notes
  ./cmd/notes` from the module root `example.com/notes`; the tests build it
  once in `TestMain` into a temporary directory.
- Scope: end-to-end only. Every test runs the built binary as a subprocess
  in a fresh temporary directory with a controlled environment.
- Stop condition: every requirement is covered or marked `uncovered` with a
  reason. You do not run the implementation.
