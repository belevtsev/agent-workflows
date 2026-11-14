# Evidence

What has been measured about this workflow, and what has not. Every number
comes from a run you can repeat with the commands under
[Reproduce](#reproduce).

## State on 2026-11-14

- **Measured:** one full local run of the six agent eval suites: 29 cases,
  one run each.
- **Result:** four suites have every case at or above the pass floor of 0.8.
  `architect` and `adversarial-tester` each have one case below it. The
  mutant gate passes. The adversarial gate fails on one case.
- **Most failed graders trace to the evals, not the agents.** See
  [Why graders failed](#why-graders-failed).
- **Changed since, not measured again:** the five causes that lay in the
  evals. See
  [Changes to the evals after this run](#changes-to-the-evals-after-this-run).
- **Not measured:** a comparison with and without the workflow, end-to-end
  tasks, token counts and run-to-run variance. See
  [What this does not show](#what-this-does-not-show).

## Local run of the agent evals, 2026-11-14

### Method

- **Source:** `main` at `7225e7e`.
- **Cases:** the six suites under [`evals/`](../evals/README.md). Each case
  dispatches one plugin agent once with a brief. Graders then check what the
  agent wrote against its contract.
- **Command:** per suite, the flags of `.github/workflows/evals.yml`; see
  [Reproduce](#reproduce).
- **Models:** `claude-opus-5-5` runs the agents. `claude-haiku-5-5` judges
  the 55 `llm` graders with three votes each.
- **Host:** Linux (Ubuntu 26.04), Go 1.26.4, Claude Code 2.1.311.
- **Differences from the workflow:** the workflow pins Claude Code 2.1.316
  and runs one case at a time with an API key. This run used a logged-in
  subscription, three cases at a time (`-j 3`), and a cost ceiling four times
  the workflow's so that no run was cut.
- **Validity check:** the trace of every run was read for its Bash results.
  Of 198 Bash calls none failed for the sandbox, and 42 `go` commands ran
  inside the runs. Three earlier attempts were discarded because the agents
  could not run Bash; [`evals/README.md`](../evals/README.md#run-locally)
  names the causes.

### Results

| Suite | Cases | At or above 0.8 | Lowest score | Mean score | Graders passed | Wall time | List price |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `spec-writer` | 3 | 3 | 0.952 | 0.984 | 55 of 56 | 3 min 29 s | 2.39 USD |
| `architect` | 4 | 3 | 0.75 | 0.906 | 31 of 34 | 16 min 10 s | 8.39 USD |
| `implementer` | 3 | 3 | 0.875 | 0.875 | 21 of 24 | 2 min 14 s | 1.35 USD |
| `test-engineer` | 4 | 4 | 0.818 | 0.909 | 43 of 47 | 10 min 34 s | 5.03 USD |
| `adversarial-tester` | 3 | 2 | 0.786 | 0.881 | 37 of 42 | 3 min 28 s | 2.34 USD |
| `reviewer` | 12 | 12 | 0.857 | 0.939 | 148 of 158 | 13 min 19 s | 8.58 USD |

Totals: 335 of 361 graders passed. The list price is 28.08 US dollars, which
is what the CLI estimates for an API key; a subscription pays in usage. The
run took 36 minutes of wall time on two tracks.

| Gate | Result |
| --- | --- |
| `TestEvalResult`, floor 0.8 for every case | Passes for `spec-writer`, `implementer`, `test-engineer` and `reviewer`. Fails for `architect` (`config-hot-reload`, 0.75) and `adversarial-tester` (`loader-once-race`, 0.786). |
| `TestMutantGate` | Passes: in all four `test-engineer` cases the authored tests pass on the implementation and fail on every mutant. |
| `TestAdversarialGate` | Passes for `frame-length-limit` and `path-prefix-escape`. Fails for `loader-once-race`: one kept test still fails after the case's fix. |

### Why graders failed

26 graders failed. Grouped by cause:

| Cause | Failed | Reading |
| --- | ---: | --- |
| `handoff-shape` | 9 | Contract and eval. Each agent wrote a finding as a string; the handoff schema and the grader want an object. The review contracts ask for "one line" per finding and name the shape only as what `cw task schema handoff` prints. A run cannot execute that command, because it grants only `go` commands. |
| `map-names-tests` | 3 | Eval or contract. The grader wants each line of the requirement map to say how its test fails. The test-engineer contract asks only for the test's file and name. |
| `self-review-cites-evidence` | 3 | Agent, at least in part. In the one self-review read in full, two checklist items have no `path:line` evidence. None lists a log path, because the run may not redirect output. The cause in the other two cases is not established. |
| `probe-run` | 2 | Eval. The grader looks for `go test`. The agents ran `go -C <dir> test`, which it does not match. |
| `no-passing-test-kept` and the adversarial gate, `loader-once-race` | 1 | Eval, probably. The agent kept a second test: a `Get` that waits on a failed load returns the loader's value with the error, where requirement R2 says `""`. The fixture does that before and after the case's fix, and the case assumes one defect. The contract does not say whether R2 covers a waiting `Get`. |
| `design-quality`, `config-hot-reload` | 1 | Eval. The rubric asks what happens when a reload races a shutdown. The case's brief and fixture have no shutdown, and the design does not treat one. Asked directly with the rubric, the judge model names that item as missing. |
| Single `llm` graders | 6 | Not established: `spec-quality`, `untested-requirement-caught`, `planted-defects-caught`, `contract-testable` (two cases) and `tests-from-contract`. Five of them judged files of 8,000 characters or more, where the CLI warns that judges are noisy. |
| `contract-read` | 1 | Not examined. |

The two cases below the floor:

- **`adversarial-tester/loader-once-race` (0.786)** failed `handoff-shape`,
  `probe-run` and `no-passing-test-kept`: the first, fourth and fifth rows
  above.
- **`architect/config-hot-reload` (0.75)** failed two `llm` graders,
  `design-quality` by three votes to none and `contract-testable` by two to
  one: the sixth and seventh rows above.

### Rerun of `architect/config-hot-reload`

The case ran three more times with the same flags. The scores were 0.875,
0.875 and 0.75, so the case falls below the floor in two of its four runs.
`design-quality` failed in all four runs, each time by three votes to none.
`contract-testable` failed in two of the four.

### Changes to the evals after this run

Five of the causes above lay in the evals. They were changed after the run.
No suite has run since, so their effect on the scores is not measured.

| Cause | Change |
| --- | --- |
| `handoff-shape` | A run can call `cw task schema handoff`, as the contracts tell the agent to: the command grants `Bash(cw task schema *)` and needs `cw` on `PATH`. |
| `map-names-tests` | The grader asks what the contract asks: per requirement a test's file and name, or `uncovered` with a reason. |
| `probe-run` | The graders that look for `go test` also match `go -C <dir> test`. |
| `loader-once-race` | In the fixture a `Get` that waits on a failed load returns `""` with the error, so the seeded defect is the only one. Requirement R3 of the case's contract now says so. |
| `design-quality`, `config-hot-reload` | The rubric no longer asks about a shutdown. |

The `self-review-cites-evidence`, single `llm` grader and `contract-read`
rows are unchanged.

### Per-case results

A case's score is the share of its graders that passed.

#### `spec-writer`

| Case | Score | Graders passed | Failed graders | Time | List price |
| --- | ---: | ---: | --- | ---: | ---: |
| `avatar-upload-gaps` | 1 | 16 of 16 | none | 3 min 15 s | 0.75 USD |
| `bookmark-export-brief` | 0.952 | 20 of 21 | `spec-quality` | 3 min 29 s | 0.89 USD |
| `link-expiry-delta` | 1 | 19 of 19 | none | 3 min 17 s | 0.79 USD |

#### `architect`

| Case | Score | Graders passed | Failed graders | Time | List price |
| --- | ---: | ---: | --- | ---: | ---: |
| `config-hot-reload` | 0.75 | 6 of 8 | `contract-testable`, `design-quality` | 7 min 56 s | 1.57 USD |
| `ledger-money-adr` | 1 | 10 of 10 | none | 14 min 52 s | 2.68 USD |
| `outbox-publisher` | 1 | 8 of 8 | none | 13 min 3 s | 2.66 USD |
| `rate-limiter-boundary` | 0.875 | 7 of 8 | `contract-testable` | 8 min 14 s | 1.49 USD |

#### `implementer`

| Case | Score | Graders passed | Failed graders | Time | List price |
| --- | ---: | ---: | --- | ---: | ---: |
| `bounded-worker-pool` | 0.875 | 7 of 8 | `self-review-cites-evidence` | 2 min 14 s | 0.52 USD |
| `idempotency-claims` | 0.875 | 7 of 8 | `self-review-cites-evidence` | 1 min 29 s | 0.42 USD |
| `parse-limits-config` | 0.875 | 7 of 8 | `self-review-cites-evidence` | 1 min 33 s | 0.43 USD |

#### `test-engineer`

| Case | Score | Graders passed | Failed graders | Time | List price |
| --- | ---: | ---: | --- | ---: | ---: |
| `clamp-claude-md-probe` | 1 | 14 of 14 | none | 2 min 33 s | 0.66 USD |
| `notes-init-e2e` | 0.818 | 9 of 11 | `map-names-tests`, `tests-from-contract` | 10 min 34 s | 2.04 USD |
| `retry-budget-matrix` | 0.909 | 10 of 11 | `map-names-tests` | 6 min 41 s | 1.36 USD |
| `ttl-cache-matrix` | 0.909 | 10 of 11 | `map-names-tests` | 4 min 39 s | 1 USD |

#### `adversarial-tester`

| Case | Score | Graders passed | Failed graders | Time | List price |
| --- | ---: | ---: | --- | ---: | ---: |
| `frame-length-limit` | 0.929 | 13 of 14 | `probe-run` | 3 min 7 s | 0.8 USD |
| `loader-once-race` | 0.786 | 11 of 14 | `handoff-shape`, `no-passing-test-kept`, `probe-run` | 3 min 28 s | 0.85 USD |
| `path-prefix-escape` | 0.929 | 13 of 14 | `handoff-shape` | 2 min 29 s | 0.71 USD |

#### `reviewer`

| Case | Score | Graders passed | Failed graders | Time | List price |
| --- | ---: | ---: | --- | ---: | ---: |
| `code-price-cache` | 0.917 | 11 of 12 | `untested-requirement-caught` | 3 min 3 s | 0.78 USD |
| `code-upload-quota` | 1 | 13 of 13 | none | 3 min 21 s | 0.78 USD |
| `code-webhook-dispatch` | 1 | 12 of 12 | none | 2 min 58 s | 0.72 USD |
| `design-job-timeout-units` | 0.857 | 12 of 14 | `contract-read`, `handoff-shape` | 2 min 17 s | 0.58 USD |
| `design-revision-scope-growth` | 1 | 11 of 11 | none | 2 min 36 s | 0.65 USD |
| `design-webhook-retries` | 0.929 | 13 of 14 | `handoff-shape` | 2 min 2 s | 0.51 USD |
| `spec-audit-export-tasks` | 0.947 | 18 of 19 | `handoff-shape` | 2 min 35 s | 0.63 USD |
| `spec-claude-md-probe` | 1 | 11 of 11 | none | 1 min 36 s | 0.46 USD |
| `spec-invoice-reminders` | 0.909 | 10 of 11 | `handoff-shape` | 1 min 54 s | 0.55 USD |
| `spec-team-invites-scope` | 0.909 | 10 of 11 | `handoff-shape` | 1 min 52 s | 0.53 USD |
| `tests-rate-limiter` | 0.933 | 14 of 15 | `handoff-shape` | 4 min 32 s | 1.12 USD |
| `tests-session-sweeper` | 0.867 | 13 of 15 | `handoff-shape`, `planted-defects-caught` | 5 min 28 s | 1.35 USD |

## What this does not show

- **No comparison with a plain agent.** The suites run one arm
  (`--ablation none`), because a baseline arm cannot dispatch the plugin's
  agents. Nothing here says a change made with the workflow is better than
  one made without it.
- **No end-to-end task.** Each case exercises one agent on fixtures. No case
  takes a change through `cw task verify`.
- **No token counts.** The result file holds list price and time only.
- **No variance.** Each case ran once. A case near the floor can land on
  either side of it.
- **No CI run.** `.github/workflows/evals.yml` has not run with an API key;
  these are local results.
- **No raw results in the tree.** The result files and traces are kept
  outside the repository.

## Reproduce

Prepare the host as [`evals/README.md`](../evals/README.md#run-locally)
says, with this checkout's `cw` on `PATH`. Then, from the plugin root, for
each suite (`spec-writer`, `architect`, `implementer`, `test-engineer`,
`adversarial-tester`, `reviewer`):

```sh
suite=architect
runs="$HOME/.cache/evals-run/$suite"
mkdir -p "$runs"
TMPDIR="$runs" claude plugin eval . --tag "$suite" --ablation none \
  --scaffold --trust-plugin --keep-temp --no-publish \
  --model claude-opus-5-5 --judge-model claude-haiku-5-5 --threshold 0.8 \
  --max-cost-usd 30 -j 3 \
  --allow-tools Write Edit "Bash(go *)" "Bash(cw task schema *)" \
  --json "$runs/result.json"
EVAL_RESULT_JSON="$runs/result.json" EVAL_PASS_FLOOR=0.8 \
  go test ./evals/ -run '^TestEvalResult$' -count=1 -v
```

For `test-engineer` and `adversarial-tester`, open the kept run directories
and run the suite's gate:

```sh
chmod 700 "$runs"/claude-eval-* "$runs"/claude-eval-*/sealed
EVAL_ARTEFACTS_DIR="$runs" go test ./evals/ -run '^TestMutantGate$' -count=1 -v
EVAL_ARTEFACTS_DIR="$runs" go test ./evals/ -run '^TestAdversarialGate$' -count=1 -v
```

The run reported above did not grant `Bash(cw task schema *)`. Every run is
a real model call on your account, and scores differ from run to run.
