# Eval suites for the agent roster

One suite per plugin agent, three scenario cases each, under `spec-writer/`,
`spec-reviewer/`, `architect/`, `implementer/` and `test-engineer/`. Every
case asks the coordinator to dispatch the plugin agent once through the Agent
tool with a brief, and grades what the agent writes (artefacts under
`artefacts/` in the run's workspace, and the spec writer's change folder)
against the agent's contract in `skills/task-orchestration/references/contracts/`.

Each case's `testdata/` holds the scenario inputs the brief names; the case's
`scaffold.sh` copies it into the run's workspace, so pass `--scaffold`. The
implementer cases also hold a `design.md`, the test-engineer cases an `impl/`
directory and one spec-reviewer case the writer's notes, which the brief
forbids passing on; `tool_used` graders fail the run if any is read.

Run from the plugin root. The agents write files and run Go checks, so grant
those tools, keep one arm (the baseline arm cannot dispatch the plugin's
agents) and set a cost ceiling:

```sh
claude plugin eval . --tag architect --ablation none --runs 1 --scaffold \
  --allow-tools Write Edit "Bash(go *)" --max-cost-usd 20 --no-publish
```

Use `--tag implementer`, `--tag test-engineer`, `--tag spec-writer` or
`--tag spec-reviewer` for the other suites and `--case <name>` for one case.
The spec suites write Markdown only; add `"Bash(openspec *)"` to let them run
`openspec validate` when the CLI is installed. Results land in
`evals/results/`, which is ignored by Git. Go sources under `testdata/` are
fixtures and are ignored by the Go toolchain.
