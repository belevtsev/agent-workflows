---
description: Implement a bounded worker pool in Go from an interface contract and brief.
tags: [implementer]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Glob, Grep, Agent, Write, Edit, Bash]
---
You are the coordinator of a task-orchestration workflow. The `agent-workflows`
plugin is loaded in this session. Dispatch its `implementer` agent exactly once
through the Agent tool (`subagent_type: "agent-workflows:implementer"`) with the
brief below, wait for it, then reply with the paths it wrote and its open
questions, and nothing else. Do none of the work yourself.

The directory `testdata/` in the current working directory holds the inputs
named below. The brief must tell the agent to read its default contract (the
agent's own definition names the path), give the absolute path of the current
working directory, and say that every path below is relative to it: inputs
under `testdata/`, the Markdown artefacts under `artefacts/`, and code or test
files exactly at their owned paths. Otherwise pass the brief verbatim.

Brief for the implementer (pass it the files `testdata/contract/interface-contract.md`
and `testdata/contract/implementation-brief.md`; do not mention or pass
`testdata/design.md`, which is the architect's design discussion):
- Goal: implement Go package `pool` as `pool/pool.go` in the current working
  directory, inside module `example.com/pool` (create `go.mod` with `go 1.22`).
- Requirements: R1 to R7 in the interface contract.
- Owned paths: `go.mod`, `pool/pool.go`, `artefacts/self-review.md`.
- Checks: `go vet ./...` and `go build ./...` run from the working directory.
- Stop condition: the change is complete, `artefacts/self-review.md` answers
  every checklist item with evidence, and every check ran on the final tree.
