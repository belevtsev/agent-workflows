# Eval suites for the agent roster

One suite per plugin agent, three scenario cases each, under
`architect/`, `implementer/` and `test-engineer/`. Every case asks the
coordinator to dispatch the plugin agent once through the Agent tool with a
brief, and grades the artefacts the agent writes under `artefacts/` in the
run's workspace against the agent's contract in
`skills/task-orchestration/references/contracts/`.

Each case's `testdata/` holds the scenario inputs the brief names; the case's
`scaffold.sh` copies it into the run's workspace, so pass `--scaffold`. The
implementer cases also hold a `design.md` and the test-engineer cases an
`impl/` directory that the brief forbids passing on; `tool_used` graders fail
the run if either is read.

Run from the plugin root. The agents write files and run Go checks, so grant
those tools, keep one arm (the baseline arm cannot dispatch the plugin's
agents) and set a cost ceiling:

```sh
claude plugin eval . --tag architect --ablation none --runs 1 --scaffold \
  --allow-tools Write Edit "Bash(go *)" --max-cost-usd 20 --no-publish
```

Use `--tag implementer` or `--tag test-engineer` for the other suites and
`--case <name>` for one case. Results land in `evals/results/`, which is
ignored by Git. Go sources under `testdata/` are fixtures and are ignored by
the Go toolchain.
