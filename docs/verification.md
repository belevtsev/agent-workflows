# Behavioral and offline verification

## Verify task behavior

Declare a behavioral check with the command the project already uses. `when`
patterns schedule relevant checks first; `expect` describes the observable
result; `verify = true` makes an applicable check part of task verification.
This is optional repository policy. Existing checks without verification
metadata keep their existing task behavior.
`verify = true` requires a nonempty `expect`. Omit `when` to apply that check
to every change; when present, patterns must be nonempty repository-relative
patterns, using the same pattern syntax as `paths`.

```toml
[[check]]
id = "login-behavior"
run = ["go", "test", "./internal/login", "-run", "TestLogin"]
when = ["internal/login/**", "cmd/server/**"]
expect = "A valid login creates a session; invalid credentials create none."
verify = true
```

Use the actual check command format already present in your `kitsu.toml`;
the equivalent CLI declaration is:

```sh
kitsu check add login-behavior --when 'internal/login/**' --when 'cmd/server/**' \
  --expect 'A valid login creates a session; invalid credentials create none.' \
  --verify -- go test ./internal/login -run TestLogin
```

`when` is a scheduling hint, separate from `paths`. `paths` remains a promise
that only those dependencies can affect a check's result, used for evidence
reuse. Never narrow `paths` merely to select a check. No mapping removes a
declared check from the final required gate, and `expect` is a description,
not an assertion engine: the declared command must actually assert it.

Inspect a plan before running it:

```sh
kitsu check plan
kitsu check plan --from HEAD~1 --json
kitsu task verify fix-login --from HEAD~1
kitsu task verify fix-login --workflow small --from HEAD~1 --json
```

`check plan` defaults to `HEAD`. It compares the chosen revision with the
complete working tree, including staged, unstaged, added, deleted and
untracked paths, and reports priority reasons, remaining full gate checks and
uncovered areas. An uncovered area means no declared change mapping
covers it; it is a gap to inspect, never proof that no checks are needed.
The plan also schedules regular files whose actual working bytes differ from
their Git-normalized snapshot, so clean filters or CRLF conversion cannot hide
a behavioral trigger. Without a historical raw-byte baseline this is
conservative: unchanged normalized originals may remain prioritized. This
does not change `paths` or the gate's existing evidence dependency rules.

The first `task verify` attempt requires an explicit `--from` revision. It pins
that resolved base for later attempts; subsequent calls can omit `--from`.
Choose the task's intended starting revision, not a convenient revision that
hides changed behavior. Finish any active managed worker interval first.
When `--workflow` is omitted, an existing full task folder selects `full`;
otherwise the command selects `small`.
An unmet verification result exits 5 (`not verified`); syntax or configuration
errors remain distinct failures. JSON and compact output preserve that outcome.

Verification runs targeted declared commands first. A targeted failure stops
the attempt and leaves remaining checks pending. On targeted success, the
coordinator command publishes a sanitized historical summary, then satisfies
all remaining required checks using current actual evidence or execution.
It never infers a pass from the chosen base. The summary is written before
the final checks so those checks cover it; there are no tracked writes after
the final checks. An unchanged successful repeat creates neither a new
summary nor a new run. Reuse also needs a matching private verification
observation of the actual filesystem bytes. The first local attempt runs
applicable required behavioral checks even if older Git-based check evidence
exists. Changed source bytes, including hidden or Git-normalized edits,
changed verification policy or selected original skill sources, or a replaced
deciding check record require fresh behavioral observation.
Before emitting a readiness result, every output format rechecks that source,
raw bytes, selected skill sources and deciding evidence remain stable.

Full summaries live under `openspec/changes/<task>/verification/`; small-task
summaries under `openspec/changes/.verification/<task>/`. Each stores immutable
`attempts/<sha256>.json` files and a `binding.json` selecting the current attempt.
These source-bound historical records are suitable for sharing. Raw logs remain local.
When a small task is promoted, a full publication moves its complete small
verification history to the full task folder; competing histories are refused.
A historical summary alone is not current readiness: readiness requires
matching current local check records and available logs. A clone without
those logs reports verification unavailable, never passed; run `task verify`
to establish actual local check evidence again. `task check`
validates shared summary structure without requiring private logs. Full-task
completion is blocked when applicable required verification is missing;
small-task stage remains `unknown` and exposes verification separately.

```sh
kitsu task verify fix-login
kitsu task verify fix-login --compact
kitsu task show fix-login --json
kitsu task check fix-login
```

`--compact` is opt-in presentation of the same verification lifecycle and
authority. It does not prove general compact/ordinary quality equivalence,
review quality, token savings or a lower bill. Independent review, human
approvals and the normal Git gate still apply. Workers may run declared
checks and report observations, but never write verification summaries;
the coordinator runs `task verify` after their intervals finish.

Read help at each depth for the current syntax and flags:

```sh
kitsu --help
kitsu check --help
kitsu check add --help
kitsu check plan --help
kitsu task --help
kitsu task verify --help
```

## Use the project's existing tools

Selected skills should tell a fresh quality engineer, developer or reviewer
which prerequisites already exist, how to start the actual application with
the project's commands, what inputs exercise the behavior, what assertions
distinguish success from failure, and how to stop services and clean up test
data. Use the original skill directory and its instructions; selection is
neither proof of loading nor proof of compliance. Kitsu does not discover or
execute skill scripts automatically and is not a service manager.

For a forwarding or serialization fix, include a value that can be lost while the operation still succeeds. In the pflag comparison, two submitted suites accepted a parser that turned `--unknown=value` into `--unknown`. A test of the returned arguments catches the defect. Put that assertion in the repository when regression coverage is part of the request, so a later developer gets the same protection.

| Project | Existing project command to declare | Behavior the command must assert |
| --- | --- | --- |
| Go service | `go test ./internal/login -run TestLogin` | Start the real handler or app through the project's fixture; assert session creation and rejection. |
| C++ application | `ctest --test-dir build -R login` | Use the existing built application and test fixture; assert its observable output and exit status. |
| JavaScript UI | `npm run test:e2e -- login` | Use the existing script's app startup and browser fixture; assert visible login outcomes and cleanup. |

These examples assume the named commands, fixtures and prerequisites already
exist. Adapt the mapping and assertion to the repository; declaring a command
does not install a tool or create missing coverage. See [selected skills](../plugins/kitsu-team/skills/managed/SKILL.md#existing-skills)
and [the managed verification workflow](../plugins/kitsu-team/skills/managed/references/verification.md).

## Offline checks without model calls

Run these checks from the repository root with Go and Git available. They use
local historical evidence, generated fixtures and fake executables; they launch
no Codex, Claude, adviser or other model. These offline tests add no CLI command
or verifier role. Compact remains opt-in, with the same model selections, routing,
permissions, review requirements and completion authority.

## Replay the historical verifier calibration

```sh
GOWORK=off go test ./internal/pilot -run '^TestVerifierCalibration$' -count=1 -v
```

This calibration asks whether deterministic charset checks detect previously
confirmed behavioral defects. It reads the actual retained contract/stdout files
under `docs/benchmarks/selective-compact-data`, verifies their hashes and checks
each observation's expected label and verdict. It does not regenerate outputs
by executing historical candidates or edit the retained study artifacts.

The fixed split uses baseline/reference evidence for development and
candidate-1/candidate-2 evidence for validation: 44 individual behavior
observations from the same task and repository. Observations overlap and
candidate-1 is identical to the reference. This is public retrospective
validation, not an unseen or blind holdout. A correct behavior observation is
a control for that individual behavior, not a claim that its whole program is
correct. Report detected known misses, false positives and malformed or unknown
evidence separately; unavailable evidence cannot count as a detected defect or
a passing behavior.

The fixture has 12 labeled defects and 32 correct behavior controls. The
expanded checks are expected to detect all 12 with no false alarms or unknown
verdicts on these retained observations. Development contains four defects and
18 controls; validation contains eight defects and 14 controls. A separate
original-coverage replay exposes 11 labeled defects outside the original
task/control coverage. That replay is not execution of the old grader and
does not change any frozen historical score.

An optional new JSON receipt can be written outside the repository:

```sh
GOWORK=off go test ./internal/pilot -run '^TestPilotVerifierCalibration$' -count=1 -v \
  -args -pilot-verifier-calibration-output=/absolute/new-verifier-calibration.json
```

Choose a new absolute `.json` output path: existing files must not be overwritten.
The receipt exposes per-observation labels/verdicts and split counts. It belongs
to this offline calibration, independently of historical
reports. This is separate from native model/token-accounting calibration and
from a live benchmark. It measures this deterministic evaluator's coverage of
these labels, not a model reviewer's general quality or defect detection rate.

## Run generated equivalence checks

```sh
GOWORK=off go test -race ./internal/contextpack \
  -run '^(TestContextOfflineReadSearchEquivalence|TestContextOfflineFreshnessEquivalence|TestContextOfflineGitEquivalence|TestContextRequiredMetadataBudgetRefusal|TestContextCursorRejectsCrossItemAndMalformed|FuzzContextReadSearchEquivalence|FuzzContextCursorValidation)$' -count=1
GOWORK=off go test -race ./internal/cli \
  -run '^(TestOfflineCoordinatorDeltaEquivalenceMatrix|FuzzWorkerReturnClaimPreservation)$' -count=1
```

Read/search checks recover generated evidence through bounded initial pages and
every required expansion, then compare it with direct source observations.
Freshness and cursor checks exercise changed source, invalid or mismatched
references and budgets too small to retain required metadata. Git checks compare
staged added/modified/deleted paths and state identity with direct Git output.
The worker-return target compares accepted claims with their input while keeping the claims
separate from local observations and authority. Coordinator checks reconstruct
bounded snapshot/delta projections and compare them with current state across
interval, check and policy changes, including cleared sections.

An ordinary `go test` runs each fuzz target's seed corpus deterministically;
it does not start a mutation campaign. The complete local regression suite is:

```sh
GOWORK=off go test -race -count=1 ./...
```

## Run bounded fuzz campaigns

Each command below starts one target with two workers for five seconds. Increase
the duration deliberately when you want a longer local campaign; keep resource
limits suited to your machine. They execute no model and incur no model charges.

```sh
GOWORK=off GOMAXPROCS=2 go test ./internal/contextpack -run '^$' \
  -fuzz '^FuzzContextReadSearchEquivalence$' -fuzztime=5s -parallel=2
GOWORK=off GOMAXPROCS=2 go test ./internal/contextpack -run '^$' \
  -fuzz '^FuzzContextCursorValidation$' -fuzztime=5s -parallel=2
GOWORK=off GOMAXPROCS=2 go test ./internal/cli -run '^$' \
  -fuzz '^FuzzWorkerReturnClaimPreservation$' -fuzztime=5s -parallel=2
```

On failure, Go prints a corpus identifier and a focused replay command. For
example, replace `<corpus-id>` with the emitted identifier:

```sh
GOWORK=off go test ./internal/contextpack \
  -run '^FuzzContextReadSearchEquivalence/<corpus-id>$' -count=1
```

Keep the failing input and use the matching package/target to replay other
failures. Corpus exploration and execution counts vary by machine and run;
a completed bounded campaign establishes only that no failure was found in
the inputs it exercised, not exhaustive equivalence.

## Interpret the evidence

Calibration tests assess known labeled observations; generated equivalence
tests assess preservation of the facts and invariants they compare. Neither
establishes arbitrary program correctness, model-review quality or general
quality equivalence between compact and ordinary workflows. Existing
[delivery replay](https://github.com/belevtsev/kitsu/blob/e9bfb4173746b6a020c1c28d4c935d211d0f17ef/docs/benchmarks/compact-delivery-report.md) and
[coordinator replay](https://github.com/belevtsev/kitsu/blob/e9bfb4173746b6a020c1c28d4c935d211d0f17ef/docs/coordinator-overhead.md#verify-locally) cover additional
scripted lifecycle and accounting behavior.

No further paid pilot is part of these checks. The
[revised paid comparison](https://github.com/belevtsev/kitsu/blob/e9bfb4173746b6a020c1c28d4c935d211d0f17ef/docs/benchmarks/compact-delivery-paid-report.md) retains
its original one-pair token accounting and quality limitations. These new local
tests do not revise that result or establish a broader token or billing benefit.

The [OpenSpec verification record](https://github.com/belevtsev/kitsu/blob/e9bfb4173746b6a020c1c28d4c935d211d0f17ef/openspec/changes/calibrate-offline-verifiers/tests.md)
maps scenarios to tests and records validation status. Repository-wide build,
vet, lint and vulnerability checks follow [contributing](https://github.com/belevtsev/kitsu/blob/main/CONTRIBUTING.md).
