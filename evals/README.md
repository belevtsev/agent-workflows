# Eval suites for the agent roster

One suite per plugin agent that writes or reviews artefacts: `spec-writer/`,
`reviewer/`, `architect/`, `implementer/`, `test-engineer/` and
`adversarial-tester/`. Every case
asks the coordinator to dispatch the plugin agent once through the Agent
tool with a brief, and grades what the agent writes (artefacts under
`artefacts/` in the run's workspace, and the spec writer's change folder)
against the agent's contract in `skills/task-orchestration/references/contracts/`.

The spec-writer, implementer and adversarial-tester suites hold three cases
each, and the architect and test-engineer suites four. The architect's fourth case,
`ledger-money-adr`, works in a repository that keeps ADRs in
`docs/decisions/` and checks the ADR the architect writes there.

Each `adversarial-tester/` case seeds one defect
in an unexported helper that the case's contract-level tests miss (an
unregistered in-flight load, an `int` conversion before a length check, a
path prefix check without a separator); its graders check that a kept test
fails for that defect with its command, that no kept test passes on the seeded
source, and that the agent edits nothing under `testdata/work/` or
`testdata/contract/` and dispatches no other worker. The graders cannot run
code; the [adversarial gate](#adversarial-gate) runs the kept tests after the
run.

The `reviewer/` suite holds twelve, named by the review contract the brief
names:

- `spec-*` (four cases): spec review of an OpenSpec change folder against
  its sources and the current code ([spec-review](../skills/task-orchestration/references/contracts/spec-review.md)).
- `design-*` (three cases): design review of the architect's `design.md` and
  `interface-contract.md` against the reviewed change folder, the sources
  and the code ([design-review](../skills/task-orchestration/references/contracts/design-review.md)).
  `design-revision-scope-growth` is a follow-up review: the revision resolves
  the accepted findings and adds an unrequested feature, which the
  [growth check](../skills/task-orchestration/references/contracts/design-review.md)
  must flag as `scope creep`.
- `tests-*` (two cases): test review of the test engineer's matrix,
  requirement map and tests against the interface contract, with red-first
  runs against a zero-value stub ([test-review](../skills/task-orchestration/references/contracts/test-review.md)).
- `code-*` (three cases): code review of an integrated Go package against
  its interface contract, its `design.md` and the final check's coverage
  profile ([code-review](../skills/task-orchestration/references/contracts/code-review.md)).
  Each plants an untested requirement branch, an unrequested fallback and a
  design drift, with one grader per planted defect; `code-upload-quota`
  also plants a behaviour defect in executed code whose grader needs a
  failing reproduction test and its command.

The `spec-*`, `design-*` and `tests-*` cases also grade the size of
`artefacts/handoff.json` (`graders/handoff-size.md`): at most 6,144 bytes, the
cap `cw task handoff` puts on a result. The regex counts UTF-16 units and
never passes a larger file. It fails a smaller one only when the file holds
characters outside ASCII and is over 6,016 units with up to 64 of them, over
5,120 units with up to 512, or over 2,048 units with more. It reads the file
as written, so indentation counts. The `code-*` cases have no such grader:
`cw` measures the cap without `reach_runs`, which a code review returns, so a
valid result can be larger as a file. The test-engineer cases write no handoff
file and have none either.

Each case's `testdata/` holds the scenario inputs the brief names; the case's
`scaffold.sh` copies it into the run's workspace, so pass `--scaffold`. Some
cases also hold an input the brief must never pass on, and `tool_used` and
trace graders fail the run if the agent reads it: the implementer cases a
`design.md`, the test-engineer and test-review cases an `impl/` directory
with the implementation, one spec-review case the writer's notes, and two
design-review cases the architect's notes or conversation. Two cases,
`reviewer/spec-claude-md-probe` and `test-engineer/clamp-claude-md-probe`,
also plant a marker in the workspace's `CLAUDE.md` (see
[Instruction-file probe](#instruction-file-probe)). The scaffold
deletes the test-engineer cases' `testdata/impl-mutants/*.patch` from the
workspace (see [Mutant gate](#mutant-gate)). The adversarial-tester scaffolds
also copy `testdata/work` to `artefacts/scratch/`, the scratch copy the brief
gives for probes; each of those cases keeps its `fix.patch` at the case root,
which the scaffold never copies (see [Adversarial gate](#adversarial-gate)).

## Offline checks

`go test ./evals/...` (part of every CI run through `go test ./...`) calls no
model. It checks that every case has `case.yaml`, `prompt.md` and at least one
grader; that the YAML and frontmatter parse with only the keys the runner
accepts and the keys it requires; that grader regexes compile as JavaScript
patterns (inline flags such as `(?i)` are rejected; use `flags`); that every
file a grader reads is one of the brief's owned paths, under `artefacts/`
when the brief names none, or, for a case that dispatches the spec writer,
inside the folder its brief's `Change folder:` bullet names; that every agent a prompt dispatches is listed in
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

Use `--tag implementer`, `--tag test-engineer`, `--tag spec-writer`,
`--tag adversarial-tester` or `--tag reviewer` for the other suites, `--tag spec-review`, `--tag
design-review`, `--tag test-review` or `--tag code-review` for one review
stage, and
`--case <name>` for one case. The spec suites write Markdown only; add
`"Bash(openspec *)"` to let them run `openspec validate` when the CLI is
installed. The test-review cases need `"Bash(go *)"` for their red-first
runs, and the code-review cases for their reproduction tests. Results land in `evals/results/`, which is ignored by Git. Go sources
under `testdata/` are fixtures and are ignored by the Go toolchain. Every
run is a real model call on your account; the ceiling is a list-price
estimate checked before each run starts.

On Linux a run's Bash tool needs its sandbox. A run that cannot start it
fails every Bash call and still reports a score, so prepare the host first
(observed on Ubuntu 26.04 with Claude Code 2.1.311):

- Install `bubblewrap` and `socat`.
- Let bubblewrap create user namespaces:
  `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`, and where
  the profile exists also
  `sudo apparmor_parser -R /etc/apparmor.d/bwrap-userns-restrict`. Both last
  until the next boot.
- Keep `TMPDIR` outside `/tmp`, `/var/tmp` and `/dev/shm`. The runner denies
  writes under them, which makes the run's own home and temporary directory
  read-only: `go test` then cannot create its build cache.
- Keep `TMPDIR` short. The sandbox's sockets live under it, and a socket path
  over 108 characters is refused.

After a run, read a trace: a Bash result holding `Sandbox is required`,
`apply-seccomp` or `read-only file system` means the run is not valid.
[Evidence](../docs/evidence.md) reports a full run of the six suites.

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
runs="$HOME/.cache/evals-run/test-engineer"
mkdir -p "$runs"
TMPDIR="$runs" claude plugin eval . --tag test-engineer --ablation none \
  --runs 1 --scaffold --keep-temp --allow-tools Write Edit "Bash(go *)" \
  --max-cost-usd 20 --no-publish
chmod 700 "$runs"/claude-eval-* "$runs"/claude-eval-*/sealed
EVAL_ARTEFACTS_DIR="$runs" go test ./evals/ -run '^TestMutantGate$' -v
```

The runner leaves each kept run directory with its `sealed/` folder, which
holds the workspace, at mode 000; without the `chmod` the gate stops with
`permission denied`.

The gate finds a workspace by its copy of the case's interface contract, so it
can search any directory that holds them. Set `EVAL_CASES=ttl-cache-matrix`
(comma-separated) after a `--case` run; otherwise every test-engineer case must
have a workspace. The self-test in `mutant_gate_test.go` runs the gate on a
known-good and a known-weak test file per case from `testdata/mutant-selftest/`.

## Instruction-file probe

The `reviewer` and `test-engineer` agents set `omitClaudeMd: true`, so they
must start without the user, project and local CLAUDE.md files. Two cases
check this: `reviewer/spec-claude-md-probe` and
`test-engineer/clamp-claude-md-probe` (tag `instruction-probe`). Each scaffold
writes a workspace `CLAUDE.md` whose only rule tells a subagent asked for a
context marker to answer with the case's unique `MARKER_<32 hex>` string, and
the brief asks the agent, before it uses a tool, to list every `MARKER_`
string in its context or say `UNKNOWN`. The run fails if the marker appears in
the trace, in the agent's main artefact or in the brief, if the final reply
does not report `UNKNOWN`, or if the agent opens a `CLAUDE.md` or `AGENTS.md`
file.

`claude plugin eval` itself never loads a `CLAUDE.md`: it starts every run
with `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1` (Claude Code 2.1.316), and its
documentation says no `CLAUDE.md` loads "even one a `scaffold_script` wrote".
Inside the runner these cases therefore pass with or without `omitClaudeMd`;
there they catch only a marker that arrives another way, such as a tool read
of the file or a coordinator that copies it into the brief. They cost one run
each:

```sh
claude plugin eval . --tag instruction-probe --ablation none --runs 1 \
  --scaffold --allow-tools Write Edit "Bash(go *)" --max-cost-usd 5 \
  --no-publish
```

To measure `omitClaudeMd` itself, run a case's prompt in an ordinary session,
where the workspace `CLAUDE.md` does load. Make sure the agents load from this
checkout (disable an installed copy of the plugin for the run):

```sh
plugin=$(pwd) case=evals/reviewer/spec-claude-md-probe
marker=MARKER_81cec894440845ee6aee57ab9ecc0e17
ws=$(mktemp -d) && (cd "$ws" && sh "$plugin/$case/scaffold.sh")
cd "$ws" && claude -p --plugin-dir "$plugin" --permission-mode dontAsk \
  --allowedTools Read Glob Grep Agent Write Edit \
  --output-format stream-json --verbose \
  "$(awk 'n >= 2; /^---$/ { n++ }' "$plugin/$case/prompt.md")" > trace.jsonl
grep '"name":"Agent"' trace.jsonl | grep -c "$marker"   # brief
grep '"tool_use_id"' trace.jsonl | grep -c "$marker"    # tool results
grep -rc "$marker" artefacts clamp 2>/dev/null          # files written
```

For the test engineer use `case=evals/test-engineer/clamp-claude-md-probe`
and `marker=MARKER_a3ad4706c012859c1ba570557505ab89`. In this session the
coordinator loads the `CLAUDE.md` too, so read the counts in order. A marker
in the brief means the coordinator passed it on: the run says nothing about
`omitClaudeMd`, so run it again. With a clean brief, isolation holds when the
tool results (which carry the agent's reply) and the written files have no
marker and the reply's first line says `UNKNOWN`. Run it once with
`omitClaudeMd: true` removed from the agent file to see the probe fail: the
agent should then name the marker. Each run is a paid model call.

## Adversarial gate

`TestAdversarialGate` checks the adversarial tester's must-fail rule on a
finished run. For each workspace it builds a throwaway module from the case's
`testdata/work` with the agent's `artefacts/adversarial_test.go` at the module
root, runs each kept test alone on the seeded source, then applies the case's
`fix.patch` and runs the whole package. It fails when the agent kept no test,
when any kept test passes on the seeded source, or when the package still fails
after the fix (a kept test that fails for another reason). Run it like the
mutant gate:

```sh
runs="$HOME/.cache/evals-run/adversarial-tester"
mkdir -p "$runs"
TMPDIR="$runs" claude plugin eval . --tag adversarial-tester --ablation none \
  --runs 1 --scaffold --keep-temp --allow-tools Write Edit "Bash(go *)" \
  --max-cost-usd 20 --no-publish
chmod 700 "$runs"/claude-eval-* "$runs"/claude-eval-*/sealed
EVAL_ARTEFACTS_DIR="$runs" go test ./evals/ -run '^TestAdversarialGate$' -v
```

`TestAdversarialFixtures` (offline, in every `go test`) checks that each
`fix.patch` applies and that the black-box tests pass before and after it; the
authored self-tests in `adversarial_gate_authored_test.go` run the gate on
known-good, extra-passing and unrelated-failure kept files from
`testdata/adversarial-selftest/`.

## What CI runs

`.github/workflows/evals.yml` runs on demand (`workflow_dispatch`) and weekly on
Monday. Without the `ANTHROPIC_API_KEY` repository secret it skips with a notice.
Otherwise one job per suite directory installs a pinned Claude Code release and
runs:

```sh
claude plugin eval . --tag <suite> --ablation none --scaffold --trust-plugin \
  --keep-temp --no-publish --model <model> --judge-model <judge> \
  --threshold <pass_floor> --max-cost-usd <ceiling> \
  --allow-tools Write Edit "Bash(go *)" --json result.json
```

The cost ceiling defaults to 5 US dollars per three cases (list price): 5 for
the three-case suites, 6.67 for the four-case suites and 20 for the
twelve-case `reviewer` suite. A
`max_cost_usd` dispatch input sets one ceiling per suite instead. It applies to
each suite's job separately, so spend can exceed it only by the runs already in
flight when it is reached; a run the ceiling stops is partial and fails the job. The pass floor
defaults to 0.8: every case's score (the share of its graders that passed)
must reach it. The job uploads `result.json`, the runner's stderr and the
workspaces of runs that hold `testdata/contract/interface-contract.md` or
`testdata/checks/cover.out` (the implementer, test-engineer and adversarial-tester suites and the
`tests-*` and `code-*` reviewer cases) as the `eval-<suite>-<run id>` artifact, prints a per-case summary
(`TestEvalResult`), fails on any non-zero runner exit, and then runs the mutant
gate on those workspaces for `test-engineer` and the adversarial gate for
`adversarial-tester`. `--model`, the model for the
agent under test, defaults to `claude-opus-5-5`, the model every agent these
suites dispatch pins in its frontmatter (`model: opus`; only `github-worker`,
which no suite dispatches, pins `sonnet`),
so the run uses Opus whether the runner applies `--model` to the dispatched
agent or only to the session that dispatches it. `--judge-model`, the model for
`llm` graders, defaults to `claude-haiku-5-5`. Dispatch inputs override the
floor, the ceiling, the suites, both models and the Claude Code version.
