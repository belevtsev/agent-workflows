# Go development and releases

`cw` and its development automation use the Go version pinned in go.mod.
First-party code, including the helpers skills call, is Go. Third-party skills under
third_party/ keep their own dependencies. No Go module vendor tree is present.
The resource-only third_party module boundary keeps bundled example Go files
out of `cw`'s build and test package discovery.

`cmd/cw` is a thin command entrypoint over `internal/cli`, which has injectable
I/O. The runtime packages are `internal/taskflow`, `internal/consult`,
`internal/projectctx`, `internal/graphify`, `internal/outcomes` and
`internal/usagelog`; `internal/codexinstall` implements `cw install --host codex`
and `cw uninstall --host codex`. `internal/gate` decides the commit gate's
answer; `cw hook pre-publish` only reads the hook JSON and prints that answer,
and `cw hook git` asks `gate.DecideGitHook` for git's own hooks,
so gate rules are unit-tested with a fake git, clock and task store. Under
Codex, which cannot ask, the hook turns an ask into a deny unless the gate sets
`ProceedWithoutAsk` on it ([Codex](hooks.md#codex)).
`internal/shellwords` splits a shell command into simple commands and words
for both the gate and `internal/usagelog`; extend it there rather than
splitting commands again.
`cmd/cwdev` holds the maintainer evaluation.

Each `cw task` subcommand has its own constructor, `newTaskXCommand(taskDeps)`,
in `internal/cli/task_<name>.go`, listed in `taskCommands`. Add a subcommand
there: reject `--dry-run` with `taskDeps.guard`, read a JSON input with
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
`cw task run` runs a check on it through `taskflow.RunCheck`, and only
`checkSpec` turns a planned check into its `proc.Spec`: change the program,
environment or bounds of every check there.

## Verification

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

Use `go test` with a focused package or test filter while developing. Tests use
temporary homes, synthetic Git repositories and local HTTP servers. They never
change the user's configuration, consult hosted decision models, or require service
credentials.

### Repository content checks

```sh
GOWORK=off go test ./internal/skillcheck
```

`internal/skillcheck` checks the repository content rather than Go code:

- each skill's `name` is at most 64 lowercase letters, digits and hyphens and
  matches its directory, and its `description` has 1 to 1024 characters;
- relative Markdown links in skills, top-level Markdown files, `docs/` and
  `.github/` resolve;
- `disable-model-invocation` in `SKILL.md` agrees with
  `policy.allow_implicit_invocation` in `agents/openai.yaml`;
- the paths in `skills-manifest.json` and the plugin manifests exist;
- the model policy loads;
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
- no first-party Markdown file (any `.md` outside `third_party/`) opens a
  `mermaid` code block, since archify is the only diagram tool;
- no line of `CHANGELOG.md` above the first released version heading is longer
  than 200 characters.

First-party automation must use Go. Script resources supplied by skills are
exempt. Do not add Python tests, environment preparation, or release shell
scripts.

Backend decision fixtures live in `internal/skillcheck/testdata/backend-skills`;
give evaluators raw inputs without their expected decisions. Model evaluations and
fresh discovery are separate from native test assertions; `ci.yml` does not call
hosted models or receive service credentials. Only the separate agent-eval
workflow does, as described under Continuous integration.

### Consultation

`cw consult` lives in internal/consult. Its tests use local HTTP servers and synthetic
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

The multilingual corpus is a skill resource, and `cwdev eval multilingual` is its
runner:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual
```

Offline tests check complete translations, withheld expected answers,
deterministic scoring and safe bounded execution using synthetic transports. Live
runs (`--live --output-dir PATH`) call only configured providers and report the
others as `not_run` with reason `not_configured`. Run the explicit hosted
evaluation separately, following the
[evaluation guide](../skills/task-orchestration/references/adviser-providers.md#multilingual-evaluation).
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
  `profile`, keep 5. `v3.0.0-rc.1` was never tagged, but the code at
  `13b6103` wrote version 3: `internal/taskflow/testdata/rc1-small-task` holds
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
| `Native Go / ubuntu-latest` | gofmt check, `go vet ./...`, `go test -race -count=1 ./...` |
| `Native Go / macos-latest` | gofmt check, `go vet ./...`, `go test -count=1 ./...` |
| `Lint and vulnerabilities` | golangci-lint v2.15.0 with [`.golangci.yml`](../.golangci.yml), then govulncheck v1.9.0 `./...`, each for Linux and then `GOOS=darwin` |
| `Plugin manifests` | `claude plugin validate .` and `claude plugin validate ./third_party/cc-skills-golang` |

The lint job builds the pinned golangci-lint with the Go toolchain that go.mod
selects, so the linter can load this module. `.golangci.yml` enables govet,
staticcheck, errcheck, ineffassign, unused, gosec and misspell, and gives the
reason for each exclusion; a suppressed finding in code carries a
`//nolint:<linter> // reason` comment. govulncheck fails the job when the
standard library of that toolchain or a dependency has a vulnerability that
the code can reach; the fix is a newer toolchain or module version. The
[contributing guide](../CONTRIBUTING.md#build-and-test) has the local commands.

The race detector runs on Linux only. Only obsolete pull request runs are
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
`internal/skillcheck` fails when the three differ.

`.github/workflows/release.yml` runs the tests on the tagged commit and then
goreleaser with [`.goreleaser.yaml`](../.goreleaser.yaml). It publishes
`cw_<tag>_<os>_<arch>.tar.gz` for darwin and linux on amd64 and arm64 (each with
`LICENSE`, `THIRD_PARTY.md` and `licenses/`), `SHA256SUMS`, and a changelog from
the commit history; `prerelease: auto` marks a tag with a suffix such as `-rc.2`
as a prerelease. The binaries are built with CGO disabled and trimmed paths, and
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

`third_party/cc-skills-golang` pins its own `version` (2.0.0) in its
`.claude-plugin/plugin.json` and `.codex-plugin/plugin.json`, and the release
pull request does not change it. When its content changes, bump that version
in the same pull request; otherwise existing users keep their cached copy.

Steps for each tag `vX` (the owner runs steps 3 to 5):

1. Open a pull request `chore(release): vX` that moves `[Unreleased]` in
   [CHANGELOG.md](../CHANGELOG.md) to `## [X] - date`, sets the three version
   strings to `X`, sets the tag in the install commands and release links of
   `README.md`, [Get cw](operations.md#get-cw), `global/AGENTS.md`, the
   `task-orchestration` skill and `hooks/hooks.json`
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
