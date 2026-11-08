# Eval suites for the agent roster

One suite per plugin agent that writes or reviews artefacts: `spec-writer/`,
`reviewer/`, `architect/`, `implementer/` and `test-engineer/`. Every case
asks the coordinator to dispatch the plugin agent once through the Agent
tool with a brief, and grades what the agent writes (artefacts under
`artefacts/` in the run's workspace, and the spec writer's change folder)
against the agent's contract in `skills/task-orchestration/references/contracts/`.

The spec-writer, architect, implementer and test-engineer suites hold three
cases each. The `reviewer/` suite holds seven, named by the review contract
the brief names:

- `spec-*` (three cases): spec review of an OpenSpec change folder against
  its sources and the current code ([spec-review](../skills/task-orchestration/references/contracts/spec-review.md)).
- `design-*` (two cases): design review of the architect's `design.md` and
  `interface-contract.md` against the reviewed change folder, the sources
  and the code ([design-review](../skills/task-orchestration/references/contracts/design-review.md)).
- `tests-*` (two cases): test review of the test engineer's matrix,
  requirement map and tests against the interface contract, with red-first
  runs against a zero-value stub ([test-review](../skills/task-orchestration/references/contracts/test-review.md)).

Each case's `testdata/` holds the scenario inputs the brief names; the case's
`scaffold.sh` copies it into the run's workspace, so pass `--scaffold`. Some
cases also hold an input the brief must never pass on, and `tool_used` and
trace graders fail the run if the agent reads it: the implementer cases a
`design.md`, the test-engineer and test-review cases an `impl/` directory
with the implementation, one spec-review case the writer's notes, and the
design-review cases the architect's notes or conversation.

Run from the plugin root. The agents write files and run Go checks, so grant
those tools, keep one arm (the baseline arm cannot dispatch the plugin's
agents) and set a cost ceiling:

```sh
claude plugin eval . --tag architect --ablation none --runs 1 --scaffold \
  --allow-tools Write Edit "Bash(go *)" --max-cost-usd 20 --no-publish
```

Use `--tag implementer`, `--tag test-engineer`, `--tag spec-writer` or
`--tag reviewer` for the other suites, `--tag spec-review`, `--tag
design-review` or `--tag test-review` for one review stage, and
`--case <name>` for one case. The spec suites write Markdown only; add
`"Bash(openspec *)"` to let them run `openspec validate` when the CLI is
installed. The test-review cases need `"Bash(go *)"` for their red-first
runs. Results land in `evals/results/`, which is ignored by Git. Go sources
under `testdata/` are fixtures and are ignored by the Go toolchain.
