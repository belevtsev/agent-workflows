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
forbids passing on; `tool_used` graders fail the run if any is read. The
scaffold deletes the test-engineer cases' `testdata/impl-mutants/*.patch` from
the workspace (see [Mutant gate](#mutant-gate)).

## Offline checks

`go test ./evals/...` (part of every CI run through `go test ./...`) calls no
model. It checks that every case has `case.yaml`, `prompt.md` and at least one
grader; that the YAML and frontmatter parse with only the keys the runner
accepts and the keys it requires; that grader regexes compile as JavaScript
patterns (inline flags such as `(?i)` are rejected; use `flags`); that every
file a grader reads is one of the brief's owned paths (or under `artefacts/`
when the brief names none); that every agent a prompt dispatches is listed in
`.claude-plugin/plugin.json`, and that the `tool_used: Agent` grader matches
it; and that each mutant applies to its fixture and builds. A failure names the
case file and the rule it breaks.

## Run locally

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
fixtures and are ignored by the Go toolchain. Every run is a real model call on
your account; the ceiling is a list-price estimate checked before each run
starts.

## Mutant gate

The graders cannot run code, so whether the test engineer's tests would catch
a broken implementation is checked after the run. Each test-engineer case has
two or three mutants, each breaking one requirement of its contract (the first
line of the patch says which). `TestMutantGate` builds a throwaway module from
the case's `testdata/impl`, puts the authored test file at its owned path, and
runs `go test` on the unmutated implementation and once per mutant. It fails
when the authored tests fail on the implementation, or when they all still pass
with some mutant applied.

Keep the run's workspaces with `--keep-temp` under a directory of your own, then
point the gate at it:

```sh
mkdir -p /tmp/evals-run
TMPDIR=/tmp/evals-run claude plugin eval . --tag test-engineer --ablation none \
  --runs 1 --scaffold --keep-temp --allow-tools Write Edit "Bash(go *)" \
  --max-cost-usd 20 --no-publish
EVAL_ARTEFACTS_DIR=/tmp/evals-run go test ./evals/ -run '^TestMutantGate$' -timeout=120m -v
```

The gate finds a workspace by its copy of the case's interface contract, so it
can search any directory that holds them. Set `EVAL_CASES=ttl-cache-matrix`
(comma-separated) after a `--case` run; otherwise every test-engineer case must
have a workspace. The self-test in `mutant_gate_test.go` runs the gate on a
known-good and a known-weak test file per case from `testdata/mutant-selftest/`.

## What CI runs

`.github/workflows/evals.yml` runs on demand (`workflow_dispatch`) and weekly on
Monday. Without the `ANTHROPIC_API_KEY` repository secret it skips with a notice.
Otherwise one job per suite directory installs a pinned Claude Code release and
runs:

```sh
claude plugin eval . --tag <suite> --ablation none --scaffold --trust-plugin \
  --keep-temp --no-publish --model <model> --judge-model <judge> \
  --threshold <pass_floor> --max-cost-usd <max_cost_usd> \
  --allow-tools Write Edit "Bash(go *)" --json result.json
```

The cost ceiling defaults to 5 US dollars per suite (list price), applied to
each suite's job separately, so spend can exceed it only by the runs already in
flight when it is reached; a run the ceiling stops is partial and fails the job. The pass floor
defaults to 0.8: every case's score (the share of its graders that passed)
must reach it. The job uploads `result.json`, the runner's stderr and the run
workspaces as the `eval-<suite>-<run id>` artifact, prints a per-case summary
(`TestEvalResult`), fails on any non-zero runner exit, and for `test-engineer`
then runs the mutant gate on those workspaces. `--model`, the model for the
agent under test, defaults to `claude-opus-5-5`, the model every agent under
`agents/` pins in its frontmatter (`model: opus`),
so the run uses Opus whether the runner applies `--model` to the dispatched
agent or only to the session that dispatches it. `--judge-model`, the model for
`llm` graders, defaults to `claude-haiku-5-5`. Dispatch inputs override the
floor, the ceiling, the suites, both models and the Claude Code version.
