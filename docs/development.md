# Go development and releases

`cw` and its development automation use the Go version pinned in go.mod.
First-party code, including the helpers skills call, is Go. Third-party skills under
third_party/ keep their own dependencies. No Go module vendor tree is present.
The resource-only third_party module boundary keeps bundled example Go files
out of `cw`'s build and test package discovery.

`cmd/cw` is a thin command entrypoint over `internal/cli`, which has injectable
I/O. The runtime packages are `internal/taskflow`, `internal/consult`,
`internal/projectctx`, `internal/doctor`, `internal/graphify`, `internal/outcomes` and
`internal/usagelog`; `internal/codexinstall` implements `cw install --host codex`
and `cw uninstall --host codex`. `internal/projectctx` captures the source
snapshot that task records and the gate bind; `internal/doctor` runs `cw doctor`
and the session-start summary. The doctor reads the credential store, Codex's
TOML config and Graphify, and `TestGateImportClosureExcludesDoctorDependencies`
keeps it and those packages out of the gate's imports. `internal/gate` decides the commit gate's
answer; `cw hook pre-publish` only reads the hook JSON and prints that answer,
and `cw hook git` asks `gate.DecideGitHook` for git's own hooks,
so gate rules are unit-tested with a fake git, clock and task store. Under
Codex, which cannot ask, the hook turns an ask into a deny unless the gate sets
`ProceedWithoutAsk` on it ([Codex](hooks.md#codex)).
`internal/shellwords` splits a shell command into simple commands and words
for both the gate and `internal/usagelog/composition`; extend it there rather
than splitting commands again. That package, behind `cw metrics import
--composition`, is experimental: it observes a Claude import through
`usagelog.Observer`, and `internal/usagelog` does not import it.
`internal/usagelog` is a core dependency: `cw task usage` imports a transcript
through it and maps the report to a task usage sample in `internal/cli`, so
`internal/taskflow` does not import it. The `cw metrics` commands stay
experimental.
`cmd/cwdev` holds the maintainer evaluation.

Each `cw task` subcommand has its own constructor, `newTaskXCommand(taskDeps)`,
in `internal/cli/task_<name>.go`, listed in `taskCommands`. Add a subcommand
there: take no positional arguments with `Args: noArgs`, read a JSON input with
`readTaskInput` and print with `writeJSON`.

`internal/fslock` is the one file lock: `fslock.Acquire` takes `flock(2)` on a
stable lock file, waits under a context and an optional bound, and the kernel
releases it when the holder dies. The credential store, the cost history, the
Graphify cache, task record writes and `cw publish` use it; take a new lock
with it rather than another `flock` or `mkdir` loop.

`internal/proc` runs an external program: `proc.Run` starts it without a shell
in a process group of its own, with an explicit environment, keeps the head of
its output and the tail of its error output, and when the run ends, normally
or at its deadline, kills the group until no member is alive. `Spec.Grace`
sends SIGTERM first and kills only after the grace, so `cw publish` gives git
time to remove its lock files; `KeepGroupOnExit` spares background work a
program started on purpose, such as a Git hook's; `Session` starts a session
rather than only a group, as Graphify needs. `cw doctor --live`, `cw publish`,
`cw graphify` and the Go facts of `cw task verify` run on it. Run a new
subprocess with it rather than another `exec.Cmd` and group kill.
`cw task run` runs a check on it through `taskflow.Env.RunCheck`, and only
`checkSpec` turns a planned check into its `proc.Spec`: change the program,
environment or bounds of every check there.

## Verification

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

As a `cw task` check, the full check is
`["go", "test", "-race", "-count=1", "-json", "./..."]` with `GOWORK=off`
exported in the shell that runs `cw task run`, and a timeout that covers the
whole suite. It runs on the working tree, so `cw task verify` can complete
before the commit; `-json` makes it the check that proves mapped tests. It
takes no coverage flags: the `internal/graphify` process tests run the test
binary again and compare its output, and fail under `-cover` (with
`GOCOVERDIR` set as well). For the code review's profile, bind a second check
with the coverage flags that names the packages the plan expects to change,
outside `internal/graphify`; a change inside `internal/graphify` gets no
profile.

Use `go test` with a focused package or test filter while developing. Tests use
temporary homes, synthetic Git repositories and local HTTP servers. They never
change the user's configuration, consult hosted decision models, or require service
credentials.
Host smokes are synthetic acceptance evidence; keep live model calls out of CI.

### Tests

`cmd/cw` builds its `cw` binary once per test run (`buildPlainCW`), so a test
runs or copies that file and never moves or changes it. Every top-level test
there calls `t.Parallel()`; a new one must own its directories and must not
call `t.Setenv`.

A task holds at most 4,096 revisions, and every load decodes and re-checks
each one. The `internal/taskflow` tests that walk a record to that limit call
`lowerRevisionLimit`, which runs them at 64 revisions, so they must not call
`t.Parallel()`. `TestShippedRevisionLimitIs4096` alone runs at the shipped
value and fails if it changes. Only that package's tests can lower the limit.

In `internal/taskflow`, a top-level test that takes 0.2 s or more calls
`t.Parallel()` unless it sets the environment, changes directory or sets a
package variable; a new slow test should do the same. The `internal/graphify`
tests stay serial: they bound process timing, and one missed its 2 s bound on
the macOS runner when they ran beside each other.

### Repository content checks

```sh
GOWORK=off go test ./internal/skillcheck
```

`internal/skillcheck` checks the repository content rather than Go code:

- each skill's `name` is at most 64 lowercase letters, digits and hyphens and
  matches its directory, and its `description` has 1 to 1024 characters;
- relative Markdown links in skills, top-level Markdown files, `docs/` and
  `.github/` resolve, and a link's `#anchor`, to the same file or to another
  Markdown file, names one of that file's headings as GitHub slugs them;
- `disable-model-invocation` in `SKILL.md` agrees with
  `policy.allow_implicit_invocation` in `agents/openai.yaml`;
- the paths in `skills-manifest.json` and the plugin manifests exist;
- the model policy loads; its shipped routing keeps Claude Code lookup on `haiku`,
  summarization on `sonnet`, and Codex `max` for planning, architecture and
  diagnosis only; the recommended Codex memory passes use `luna`; and every
  Claude Code profile's model has an Agent alias that `references/hosts.md` names;
- the pack manifests under `packs/` load, name only evidence rules built into
  `cw`, reference skills that exist, and equal the copies embedded in `cw`;
- outside `skills/` and `third_party/` there is no `.sh`, `.py`, `.bash`, `.zsh`
  or `requirements*.txt` file and no `#!` script for `sh`, `bash`, `zsh` or
  Python, except each eval case's `evals/**/scaffold.sh` that `claude plugin eval`
  runs; first-party skills hold no `.py` file; and the only file under `.agents`
  is `.agents/plugins/marketplace.json`;
- every registration holds at least one `SKILL.md`, and `archify`, `code-review`,
  `graphify`, `task-orchestration` and `test-strategy` exist and stay
  discoverable;
- the skill routing and import-provenance fixtures under
  `internal/skillcheck/testdata` hold their contracts;
- each file of an imported skill matches the SHA-256 and the executable bit
  recorded in `third_party/personal-skill-imports.json`; the other permission
  bits follow the checkout's umask and are not compared;
- no first-party Markdown file (any `.md` outside `third_party/`) opens a
  `mermaid` code block, since archify is the only diagram tool;
- no line of `CHANGELOG.md` above the first released version heading is longer
  than 200 characters;
- each `*.md` file under `skills/task-orchestration` stays within its cap in
  the table of `orchestration_budget_test.go`, and the files within the sum cap.
  The caps are a ratchet under 10,240 bytes a file (8,192 a role contract): a
  cut lowers its row and the sum, a new file needs a row;
- the files a default `lite` task loads from that skill (`SKILL.md`, the
  references every task reads and the four lite contracts, listed in
  `lite_flow_budget_test.go`) hold at most 90,000 bytes together; every other
  file is listed there with the condition that loads it;
- only `global/AGENTS.md` states when a task is substantial; the skills, agents,
  hooks, docs and model policies link to it;
- in `.github/workflows/ci.yml` every job that uses the `go-test` action runs
  each shard the action selects, with no `exclude` list, step condition or
  `continue-on-error`, and each required `Native Go` check runs with `always()`
  and fails unless the result of its system's shard job is `success`.

First-party automation must use Go. Script resources supplied by skills are
exempt. Do not add Python tests, environment preparation, or release shell
scripts.

Backend decision fixtures live in `internal/skillcheck/testdata/backend-skills`;
give evaluators raw inputs without their expected decisions. Model evaluations and
fresh discovery are separate from native test assertions; `ci.yml` does not call
hosted models or receive service credentials. Only the separate agent-eval
workflow does, as described under Continuous integration.

### Consultation

`cw consult` lives in internal/consult; the validated record summary and the
adviser predicates the task flow needs live in the leaf package
internal/consult/record. Its tests use local HTTP servers and synthetic
records to check configured participation (including `not_configured` and
`no_advisers_configured`), deadlines, partial failures, and evidence-bound
adjudication without service keys:

```sh
GOWORK=off go test ./internal/consult
```

internal/consult/testdata holds goldens for version-4 records, wire requests,
digests and validation messages. They freeze the current record contract, so a
change there is a format change. Only the current version is readable; earlier
record versions are rejected with an error that names version 4.

Consultation records evolve separately and have one current format, version 4,
whose providers are exactly the configured participation set. Model policies must
be version 3. Tests must distinguish `not_configured`, unavailable, partially
refused and all-refused advice, cover `no_advisers_configured` without network
access, and reject task checkpoints that differ from the task's adviser binding.
They must also enforce the adjudication rules for typed evidence, investigations
and untrusted agreement.

### Multilingual evaluation

The multilingual corpus lives in `evals/multilingual/`, outside the shipped skill,
and `cwdev eval multilingual` is its runner:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual
```

Offline tests check complete translations, withheld expected answers,
deterministic scoring and safe bounded execution using synthetic transports. Live
runs (`--live --output-dir PATH`) call only configured providers and report the
others as `not_run` with reason `not_configured`. Run the explicit hosted
evaluation separately, following the
[evaluation guide](advisers.md#evaluate-multilingual-judgments).
Keep its reports outside source, freeze corpus/oracle hashes before requests, and
retain incorrect or unavailable results without automatic retries. A live baseline
measures only its labeled cases; it is not a CI assertion or production-quality
qualification.

### Tasks, diagnostics and metrics

Global project diagnostics, task evidence and measured-outcome aggregation live in
native Go packages. Tests use isolated multi-language repositories and owned child
processes. Protect project-independent dispatch, dirty/untracked/branch source
identities, stale completion rejection, actual failed/cancelled checks,
consultation task/phase/source bindings, safe external record storage and metric
unknowns/comparability. Metadata-only doctor checks never invoke optional tools or
hosted providers. Real-project read-only diagnostics, synthetic acceptance pilots
and productivity comparisons are separate evidence; preserve that distinction in
reports.

`internal/taskflow` declares the ports its use cases read, where they are
used: `Source` captures the selected source (production: `projectctx`, which
runs git), `Clock` gives every time a task record stores, and the rules read a
task's logs, results, consultations, copies, contracts and bundles through
`Artifacts`. `Consultation` revalidates a consultation record (production:
`consult.ValidateRecord`); taskflow reads only the summary in
`internal/consult/record` and never imports `internal/consult` or its HTTP
providers, which `TestTaskflowImportClosureExcludesAdviserProviders` enforces.
Each use case is a method on `taskflow.Env`, whose zero value is the production
`Source` and clock; a nil `Consultation` refuses every consultation check, so
the task commands set it in `cli`'s `taskEnv`. `cli.Config.Now` is the clock of
the task commands, the gate and `cw publish`. A rule test passes a fake
`Source`, clock or `Artifacts` instead of building a Git repository or waiting.

`cw task verify` runs a list of rules (`internal/taskflow/verify_rules.go`), in
a fixed order. Each rule returns findings: a stable `FindingCode` and the
message that the JSON output prints in `problems` or `limitations`. A new
check is a new rule, or a new code in an existing one. Code that picks out
entries, such as the `--summary` view, selects them by code, never by text, so
rewording a message changes no caller. The JSON output and the task record
keep the text only.

A caller tells a `taskflow` refusal from a failure with `errors.Is`, never by
its message: `ErrTaskLocked`, `ErrAbandoned`, `ErrRevisionLimit` and
`ErrInvalidInput` (input a use case refuses before it writes; a damaged stored
record does not match it), beside `ErrIncomplete`, `ErrCheckFailed`,
`ErrNewerRecord` and `ErrLegacyRecord`. Each matches however deeply `cli`
wraps it; typing a refusal keeps its message and its
[exit status](operations.md#exit-statuses). A test asserts on the sentinel when
the kind is its point, and on the text only where the text is the contract.

Task records hold the adviser binding, typed worker assignments, results and
coordinator acceptance; their versions follow
[Task record format](#task-record-format). Tests must cover wrong
task/brief/agent bindings, ownership overlap and unsafe paths, false check
claims, incomplete or cancelled work, finding dispositions, independent review
identity, and stale source/check/worker evidence.
Substantial-change gates apply to new tasks; small and unchanged read-only tasks
keep their existing path. Use project-neutral fixtures, including a non-Go
project, for API and CLI coverage. Worker records remain separate from adviser
records.

Changes to the independent test-engineer workflow also need bounded Codex and
Claude smokes outside CI: give fresh authors only an approved contract and test
scaffold, retain their authored patch identity, then execute their tests against
unseen correct and defective implementations. Inspect initial context, available
tool transcripts and effective model/effort; report enforcement and telemetry
limits explicitly. Exercise red-test handoffs, ownership conflicts, missing
contracts/seams, changed interfaces, contaminated feedback and prohibited assertion
weakening. These are synthetic workflow checks, not measured project-quality
improvements, and they must not modify personal host configuration.

### Task record format

A task record's `version` says which `cw` can read it. `cw` writes
`taskflow.Version` (5) and reads `taskflow.MinVersion` (3) to `Version`. It
loads strictly: a member it does not know fails the decode, and a value that a
validated member did not allow before, such as a new `test_rules` set, check
run `status` or context phase, fails validation with "invalid stored …".

- Once a release has written version N, a new member or a new value of a
  validated member (any change that a `cw` reading version N could not load,
  by decode or by validation) makes the next release write N+1. The pull
  request that adds the first such change bumps `Version` to N+1; later ones
  before the next release keep N+1. The bump goes under Breaking in the
  CHANGELOG and gets upgrade steps in the release notes
  ([Releases](#releases)). Raise `MinVersion` only to drop a version on purpose.
- Every new revision is written with `Version`, so the first revision this `cw`
  appends to a record an older `cw` started raises its version. From then on
  that older `cw` cannot read the record: the hook of a `cw` older than
  v3.0.0-rc.2 skips the task instead of gating it, and the hook of v3.0.0-rc.2
  or later asks before each commit and push. Run the same `cw` on every host
  and hook that shares a task store.
- A revision's `updated_at`, and every time it adds (an assignment, handoff,
  acceptance, approval, amendment, check start, usage sample or abandonment),
  is the later of the clock and the previous `updated_at`, and a check's
  `finished_at` is no earlier than its `started_at`. A wall clock that
  steps back therefore cannot leave a stored time after the next revision's
  `updated_at`, which the worker and amendment rules refuse. Until the clock
  catches up, stored times run ahead of it, and the gate counts such a record
  as just written ([Which records gate](hooks.md#which-records-gate)).
  `created_at` and a verification's `checked_at` are the clock's own reading:
  the gate compares them with each other across records, and no record rule
  orders `checked_at` against `updated_at`.
- The hook selects a task by the newest revision it can decode, reading
  `plan.kind`, `baseline.root` and `updated_at` without loading the chain, so
  that an older hook can still ask about a record it cannot read. Those three
  members keep their names and shapes in every version. The other members it
  reads (`plan.title`, `baseline.selected_path`, `baseline_manifest.sha256`,
  `verification` and `abandonment`) keep shapes that decode, or the hook judges
  the task by an older revision.
- A revision above `Version` is refused with `taskflow.ErrNewerRecord`, which
  says to upgrade `cw`. A record that lacks `packs`, `test_rules` or
  `baseline_manifest`, or binds the removed rule `go.package-test/v1`, is
  refused with `taskflow.ErrLegacyRecord`, which says to start a new task. A
  `cw task` command on either record exits 4;
  `cw publish --task` reports `task-unreadable` and exits 1. A version below
  `MinVersion`, or a revision with a lower version than the one before it, does
  not load either, and exits 1.
- The version each release writes (release step 1 appends a line):
  - v3.0.0-rc.1 (`13b6103`, not published): 3.
  - v3.0.0-rc.2: 4.
  - v3.0.0-rc.3 (in development): 5.

  The check run member `go_env` (F1) was the first record change after rc.2
  and bumped `Version` to 5; later changes before rc.3, such as R1's
  `profile`, keep 5. Members version 5 adds so far: on a check run, `go_env`
  (F1) and its `c` (F18), `source_sha256` and `env_key` (X1), `modes_sha256` (X2); on a worker acceptance, `source_sha256` (X2); on a worker result,
  `source_sha256` (X1) and, in its result, `report` (H1); on the record, `profile`, and on its plan,
  `profile` and `budget` (R1), and `usage` (C1); on a worker brief, `stage`,
  `fixes` and `repeats` (R2); on a worker assignment, `owner_approvals` (R3);
  on a worker finding, `reopens`, and on a worker assignment, `source_sha256`
  (R4); on the record, `owner_approvals` for the cost budget (C2).
  `v3.0.0-rc.1` was never tagged, but the code at `13b6103` wrote version 3: `internal/taskflow/testdata/rc1-small-task` holds
  a record it wrote, which must keep loading and verifying.

### Graphify

`cw graphify` lives in internal/graphify:

```sh
GOWORK=off go test ./internal/graphify
```

Its synthetic repositories and fake local runtime verify tracked-source scope,
cache identity, stale/incomplete refusal, fixed CLI modes, and environment
isolation without installing Graphify or using hosted models. A real local smoke
check with the pinned runtime verifies extraction and query behavior separately;
it does not establish resolved Go call paths. Keep generated graph caches outside
source. The [runtime setup](operations.md#optional-graphify-runtime) is explicit
per device; `cw` must never provision it.

### Archify

To change a diagram under `docs/diagrams/`, edit its JSON spec, render it with
archify `finalize`, export the SVG from the archify viewer (**Export → SVG ·
Auto**) and commit that SVG with the spec; see
[Diagrams](../CONTRIBUTING.md#diagrams) for the command.

With Node.js available, run the bundled portable Archify tests from its package:

```sh
node --test third_party/archify/test/*.test.mjs
```

This includes available Chrome/Chromium browser gates. Missing browser or optional
generator packages must be reported separately; skipped gates are unverified.
Keep render output outside the package. Archify's update helpers must make no HTTP
requests, cache writes, or installed-file changes. Hash/runtime closure and
preserved licenses are covered by the import-provenance checks in
`internal/skillcheck`; `cw` never provisions installed prerequisites.

## Continuous integration

`.github/workflows/ci.yml` runs on pushes to main and on pull requests:

| Job | Steps |
| --- | --- |
| `Go shard 1 of 4 / <system>` | the tests of `internal/taskflow` |
| `Go shard 2 of 4 / <system>` | gofmt check, `go vet ./...`, then the tests of `cmd/cw` |
| `Go shard 3 of 4 / <system>` | the tests of `internal/cli` and `internal/langpack` |
| `Go shard 4 of 4 / <system>` | the tests of every other package |
| `Native Go / ubuntu-latest`, `Native Go / macos-latest` | the required checks: each passes only when all four shards of its system passed |
| `Lint and vulnerabilities` | golangci-lint v2.15.0 with [`.golangci.yml`](../.golangci.yml), then govulncheck v1.9.0 `./...`, each for Linux and then `GOOS=darwin` |
| `Plugin manifests` | `claude plugin validate .` and `claude plugin validate ./third_party/cc-skills-golang` |

The lint job builds the pinned golangci-lint with the Go toolchain that go.mod
selects, so the linter can load this module. `.golangci.yml` enables govet,
staticcheck, errcheck, ineffassign, unused, gosec and misspell, and gives the
reason for each exclusion; a suppressed finding in code carries a
`//nolint:<linter> // reason` comment.

It also enables `gocognit` (above 60), `funlen` (above 120 lines, comments not
counted) and `gocyclo` (above 30). Today's offenders are listed one rule per
function at the end of the file, so a new offender fails the job; the list only
shrinks, and splitting a listed function removes its rule. errcheck no longer
hides unchecked `Close`, `Remove` and `fmt.Fprint*` results everywhere: the
existing ones are excluded per package with a reason, and fixing a package's
sites removes its rule. govulncheck fails the job when the
standard library of that toolchain or a dependency has a vulnerability that
the code can reach; the fix is a newer toolchain or module version. The
[contributing guide](../CONTRIBUTING.md#build-and-test) has the local commands.

The shards run for `ubuntu-latest` (`go test -race -count=1`) and
`macos-latest` (`go test -count=1 -timeout=15m`), so the race detector runs on
Linux only. [`.github/actions/go-test`](../.github/actions/go-test/action.yml)
holds their steps and the package lists of shards 1 to 3; a new package
lands in shard 4 without a change. Only obsolete pull request runs are
cancelled. `ci.yml` makes no release and receives no service credentials.

`.github/workflows/evals.yml` is separate: it runs the paid `claude plugin eval`
suites under `evals/` on demand and weekly, receives the `ANTHROPIC_API_KEY`
secret, and skips with a notice when that secret is not set. It never runs on
pushes or pull requests. See `evals/README.md`.

## Releases

A release is published from a pushed `v*` tag, never from a push to main, and
only the project owner pushes tags. The tags are `v3.0.0-rc.2`, the first
published v3 release candidate (`v3.0.0-rc.1` was never tagged), then further
release candidates (`v3.0.0-rc.3` and so on), then `v3.0.0`. The plugin manifests carry the tag without
its leading `v`: `version` in `.claude-plugin/plugin.json` and
`.codex-plugin/plugin.json`, and `metadata.version` in
`.claude-plugin/marketplace.json`. `TestPluginVersionsAgree` in
`internal/skillcheck` fails when the three differ. What each version number promises is in
[Stability](operations.md#stability).

`.github/workflows/release.yml` runs the tests on the tagged commit and then
goreleaser with [`.goreleaser.yaml`](../.goreleaser.yaml). It publishes
`cw_<tag>_<os>_<arch>.tar.gz` for darwin and linux on amd64 and arm64 (each with
`LICENSE`, `THIRD_PARTY.md` and `licenses/`), `SHA256SUMS`, and a changelog from
the commit history that leaves out `docs`, `test` and `chore` commits, with or
without a scope (a breaking one, marked `!`, stays); `prerelease: auto` marks a
tag with a suffix such as `-rc.2` as a prerelease. The binaries are built with CGO disabled and trimmed paths, and
`cw version` reports the tag, commit and commit date.

The plugin marketplace installs from the default branch, and the
agent-workflows manifests pin `version` to the last tag. On Claude Code a new
install of agent-workflows copies `main` as it stands that day. An existing
install keeps its cached copy until the `version` string changes, which only
the release pull request (step 1 below) does, so existing users move from one
release to the next and never see `main` between them
([versions and updates](https://code.claude.com/docs/en/plugins/loading#versions-and-updates);
from the vendor documentation, not tested here). Every merge must still leave
`main` releasable, because new installs take it.

`third_party/cc-skills-golang` pins its own `version` (2.0.1) in its
`.claude-plugin/plugin.json` and `.codex-plugin/plugin.json`, and the release
pull request does not change it. When its content changes, bump that version
in the same pull request; otherwise existing users keep their cached copy.

Steps for each tag `vX` (the owner runs steps 3 to 5):

1. Open a pull request `chore(release): vX` that moves `[Unreleased]` in
   [CHANGELOG.md](../CHANGELOG.md) to `## [X] - date`, sets the three version
   strings to `X`, sets the tag in the install commands and release links of
   `README.md`, [Get cw](operations.md#get-cw), `global/AGENTS.md`, the
   `task-orchestration` skill, `hooks/hooks.json` and `hooks/codex-hooks.json`
   (`TestInstallCommandsNameTheRelease` fails when one names another tag),
   lists upgrade steps for any task record version change, and appends the
   record version the release writes to [Task record format](#task-record-format).
2. After it merges, confirm CI is green on that commit.
3. Tag and push: `git tag vX && git push origin vX`. `release.yml` runs the
   tests and goreleaser.
4. Verify the release: download one darwin and one linux archive and check
   them against `SHA256SUMS`; run `cw version` and confirm the tag, commit and
   date; run `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@vX`
   with a clean module cache; install the plugin on a clean profile, start
   a session and confirm its context has no `plugin X, cw Y` line
   ([session start](hooks.md#session-start)).
5. Edit the GitHub release notes: three sentences on what changed, the
   Breaking list, the upgrade steps, and known limitations with a link to the
   [threat model](../SECURITY.md#threat-model).

Keep credentials, machine state, evaluations, and audit outputs outside source.
Preserve all licenses/notices and maintain THIRD_PARTY.md. The manifest defines
36 registrations and 81 entrypoints.
