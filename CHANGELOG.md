# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Breaking

- Pass `--task DIR` to every `cw task` command that takes it: the working directory is no longer used as the task,
  and a command without `--task` exits 2 with `--task DIR is required` ([exit statuses](docs/operations.md#exit-statuses)).
- The Go rule credits a `go test` run only under the `GOFLAGS`, `GOOS` and `GOARCH` its go command reports with `go env`, and task
  records move to version 5: rerun Go checks recorded before, and upgrade every `cw` sharing the store ([which go](docs/operations.md#which-go-a-check-runs)).
- `cw consult` sends a task-bound request only when its task, not abandoned, holds that task context in the store the hook reads,
  so a request cannot name advisers its task did not bind: for a `--store DIR` task, set `CW_TASK_STORE=DIR` ([consultation](docs/operations.md#consultation-command)).
- `cw task` `start`, `run`, `assess`, `assign`, `handoff`, `accept`, `consult`, `verify` and `add-checks` print their one-line summary by default:
  a script that reads the whole record passes `--full`; `--summary` is still accepted ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- `cw task show` prints the task's state and field names (`--full` for the record), `cw task timeline` the last 20 entries (`--last 0` for all),
  and the `verify` summary joins like problems and lists at most 20 ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- `cw task` summaries and the commit gate's deny reason name the task by `task_id` instead of its directory, and a log path is relative
  to the task directory: pass the ID to `--task`; `--full` keeps the record ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- Codex runs the plugin's new `hooks/codex-hooks.json` (`cw hook … --host codex`) instead of `hooks/hooks.json`: trust the hooks again in
  `/hooks` after updating, or Codex runs none; a given `--host` decides the host over the hook input ([Codex hooks](docs/hooks.md#codex)).

- While a task that passed `cw task verify` is open, both commit gates deny a `git commit` recording staged bytes or a mode that differ
  from the verified working tree for a path it changes, and ask about one a git filter staged ([which source counts](docs/hooks.md#which-source-counts)).

### Added

- `cw task start` fixes a profile (`lite` by default, `standard` or `strict`) in the task record and prints it as `profile` in its summary, with
  stages, round caps and a provisional dollar budget a plan's `budget` may lower but not raise; `cw task assign` enforces the round caps
  and fix rounds, and it and `cw task run` stop new work at the budget's stop ([profiles](docs/operations.md#profiles)).
- `cw task assign` refuses a round past its stage's cap with exit 4 and no revision, unless `--owner-approval "<the owner's words>"`
  records the owner's words on the assignment; the plugin hook asks the user to confirm them ([round caps](docs/operations.md#round-caps)).
- `cw task assign` refuses a third fix round on a finding or requirement still open (exit 4); the owner decides, by words given with
  `--owner-approval escalation=…`, between one more round and a new approach, or disposes of the finding ([fix rounds](docs/operations.md#fix-rounds)).
- At 80% of the task's estimated dollar budget, `cw task assign` and `cw task run` exit 4 and ask the owner, whose `cost-budget` words go on
  the task; the plugin hook samples Claude Code and Codex usage before `cw task` commands ([budget stop](docs/operations.md#budget-stop)).
- Every `cw task` command takes the task ID for `--task` as well as the directory, found in the default task store
  or in `$CW_TASK_STORE`, so a call need not repeat the absolute path ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- `cw gate install` also writes git's `pre-merge-commit` hook, so a merge commit without conflicts meets the gate as a commit does;
  run it again where an older `cw` wrote the hooks, which `cw doctor` and the session-start line then name ([git hooks](docs/hooks.md#git-hooks)).
- Each check run records the digest of the source it ran at and a key of its go and `env_passthrough` configuration, keyed per task,
  and each worker result its source digest, so a later pass can be matched to unchanged bytes ([run keys](docs/operations.md#what-a-check-run-records)).
- `cw task assign` records the stage a brief answers and, for a later round, the findings or requirements it fixes or the assignment it repeats;
  under a profile it refuses an implementation or review brief without them, naming the member ([assign](skills/task-orchestration/references/worker-handoffs.md#assign-before-dispatch)).
- A passed check run stays credited across commits and merges that keep the source bytes and modes, go command, `GOFLAGS` and run key, and
  `cw task run` then reuses it without running, except a `git` check; `--force` or `CW_NO_REUSE=1` runs it ([reuse](docs/operations.md#reusing-a-passed-run)).
- `cw task usage` samples a session's tokens into the task record with dollars estimated from a dated built-in price table (or `--prices`);
  only requests since the task's start count, task usage sums each session's latest sample, and what cannot be priced stays unknown, never 0 ([cost budget](docs/operations.md#cost-budget)).
- Under a profile `cw task handoff` caps a result at 6 KiB, its summary at 1,200 bytes and each finding, reach-run or `uncovered` text
  at 400, and stores the report its `report` member names as `report-<assignment>.md`, checked on every load ([handoff caps](skills/task-orchestration/references/worker-handoffs.md#handoff-caps)).
- `cw task brief --stage S` prints, in at most 4 KiB, what a fresh worker of a stage starts from, and `cw task assign` refuses an `agent_id`
  that worked on another stage unless the brief `repeats` an assignment ([stage briefs](docs/operations.md#stage-briefs)).
- `cw task receipt` prints, in at most 4 KiB, a task's rounds, owner approvals, estimated dollars and tokens (or unknown, never 0),
  checks, findings and wall time; `verify`, `abandon` and a budget stop print it on stderr ([task receipt](docs/operations.md#task-receipt)).
- `cw task list` prints this repository's tasks with their state (open, verified, abandoned, stale or unreadable), as text or with `--json`;
  the commit gate's reasons point at it for the tasks past the three they name ([listing tasks](docs/operations.md#listing-tasks)).

### Changed

- `cw --help` lists the core commands apart from the experimental ones (`consult`, `graphify`, `keys`, `metrics`), which semver
  does not cover; one page states what each version number promises ([stability](docs/operations.md#stability)).
- `cw doctor --summary` lists under `problems`, and turns `attention` for, only what blocks the workflow (git, `cw` on `PATH`, the skills);
  optional tools are counted in `optional_missing`, and `--all` lists them ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- The `pre-merge-commit` hook lets a merge commit through during an unverified task when a remote-tracking ref contains every
  commit it takes in, so merging the fetched base branch, or `git pull`, keeps working; a merge of a local branch still stops ([git hooks](docs/hooks.md#git-hooks)).
- The docs say which usage store each `cw metrics` command reads and which wins when they differ; the context composition moves to
  its own experimental package, so the usage import stands alone, with `cw metrics` output unchanged ([usage stores](docs/operations.md#usage-stores)).
- Each `cw task` subcommand is built by its own constructor in its own file, so contributors change one command
  without touching the others; help, flags, output and exit statuses are unchanged ([development](docs/development.md)).
- `cw doctor` lives in `internal/doctor`, so the commit gate no longer imports the credential store, Graphify or go-toml;
  `cw doctor` counts a Graphify runtime as `graphify_cli` only when you may execute it ([development](docs/development.md)).
- Task rules read the source, the clock and task artefacts through ports (`taskflow.Env`, `cli.Config.Now`),
  so a rule test uses fakes instead of a Git repository or waiting ([ports](docs/development.md#tasks-diagnostics-and-metrics)).
- Task rules check consultation records through a validator port and the `internal/consult/record` summary, so
  `internal/taskflow` no longer links the adviser HTTP providers or `net/http` ([ports](docs/development.md#tasks-diagnostics-and-metrics)).
- `taskflow` types its refusals (locked, abandoned, revision limit, invalid input), so code and tests tell them from failures with
  `errors.Is`; messages, JSON output and exit statuses are unchanged ([typed errors](docs/development.md#tasks-diagnostics-and-metrics)).
- Cheaper default models for simple work: Claude Code lookups run on Haiku 5.5 and summaries on Sonnet 5.5; Codex Sol runs at `high`, keeping `max`
  for planning, architecture and diagnosis, and recommended Codex memory passes use Luna with no context window override ([routing](docs/workflow.md)).
- `cw task verify` runs a list of rules whose findings carry stable codes, and `--summary` selects by code, so rewording a
  message no longer returns a fixed caveat to `verify` or `doctor` summaries; JSON output is unchanged ([rules](docs/development.md#tasks-diagnostics-and-metrics)).
- `cw task verify` names an unresolved, stale or invalid decision "coordinator decision" instead of "Codex task decision", since either host
  may coordinate; the problem's place, exit status and JSON keys are unchanged ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).

### Fixed

- `cw task start` and `cw consult` no longer fail when only some adviser key variables are set and the configuration directory
  is not owner-only: they skip the saved keys, use the environment ones and warn ([adviser keys](docs/operations.md#adviser-keys)).
- `cw publish --help` says a run appends its `started` entry only once its checks pass, so a run they deny or refuse
  appends none, as the docs do ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- `go test ./internal/skillcheck` passes in a checkout made under umask 002: it checks an imported file's bytes
  and executable bit, not its other permission bits ([content checks](docs/development.md#repository-content-checks)).
- `go test ./internal/cli` passes wherever the checkout sits: the relative `--home` test resolves against the working directory
  the command is given, as `cw` does, not the test's own ([paths](docs/operations.md#paths)).
- Release notes also leave out scoped `docs`, `test` and `chore` commits, such as `chore(release):`; a breaking one
  (`chore!:`) stays ([releases](docs/development.md#releases)).
- Three links in the Go documentation and gopls skills now land on their section, and `go test ./internal/skillcheck`
  checks each Markdown `#anchor` against the target's headings ([content checks](docs/development.md#repository-content-checks)).
- The docs say what a refused `cw publish` leaves: nothing when its checks refuse, even for one file of the set, and the commit
  or push already done when a later step refuses ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- A failed `cw task bundle` write leaves no partial bundle, and the bundle is still written on a filesystem
  without hard links ([diagnostics][diag]).
- The hook docs record live runs: Claude Code asks before `cw task aban''don` too, so it is no longer listed as a bypass, and Codex
  runs both handlers and adds the conventions again on each resume ([recorded host runs](docs/hooks.md#recorded-host-runs)).
- The hook's asks about a record cw cannot read, a `cw task abandon` and a push of uncommitted changes name at most
  three tasks and count the rest, as the deny does ([asking](docs/hooks.md#asking-instead-of-denying)).
- `cw publish` refuses a remote Git hands to a remote helper (`foo::…`, `remote.<name>.vcs`, or a scheme other than https, http, ssh,
  git+ssh, ssh+git, git or file in lower case) as 4 `remote-split` ([publish](docs/operations.md#global-project-diagnostics-and-evidence)).
- Resuming a Codex thread no longer adds the conventions again, nor a second `cw gate install` line beside the compaction that
  can follow; startup and compaction still add them ([session start](docs/hooks.md#session-start)).

## [3.0.0-rc.2] - 2026-11-12

The first published v3 release candidate (`v3.0.0-rc.1` was never published): every change since v2.0.96.

### Upgrade steps

1. From v2.x there is no upgrade path: install v3 fresh, keep `workflow-tasks/`, `credentials.json` and `preferences.json`,
   and remove other v2 files by hand ([install][install], [paths](docs/operations.md#paths)).
2. Finish open v2 tasks first, or move their directories out of the task store: v3 refuses records from cw v2.0.66 to v2.0.96,
   and an open one gates commits and pushes until moved ([which records gate](docs/hooks.md#which-records-gate)).
3. This release writes task record version 4: run the same `cw` on every host and hook that shares a task store
   ([format](docs/development.md#task-record-format)).
4. Exit statuses follow one table, and several changed: update scripts that test them ([exit statuses](docs/operations.md#exit-statuses)).
5. Under Codex, trust the plugin hooks in `/hooks` after installing or updating the plugin: trust follows the hooks' hash,
   and `hooks/hooks.json` changed ([Codex](docs/hooks.md#codex)).
6. Optionally, run `cw gate install --repo <path>` in each repository, so git runs the gate for commits and pushes
   from scripts, aliases and terminals too ([installing](docs/hooks.md#installing-the-git-hooks)).

### Breaking

- The v2 installer is removed: install the Claude Code or Codex plugin instead; `cw install --host claude`
  now points at the plugin ([install][install]).
- v2.x is end-of-life: no fixes, no upgrade or cleanup path. Install v3 fresh, keep `workflow-tasks/`,
  `credentials.json` and `preferences.json`, and remove other v2 files by hand ([paths](docs/operations.md#paths)).
- `cw` refuses task records from cw v2.0.66 to v2.0.96 (no `packs` or `test_rules`), and an open one gates until moved:
  finish open v2 tasks before upgrading, or move them out of the task store ([which records gate](docs/hooks.md#which-records-gate)).
- The module path gains `/v3`: install with `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.2`
  and import `github.com/belevtsev/agent-workflows/v3/...` ([get cw][getcw]).
- Worker agents lose the `cw-` prefix and policy role `testing` is split by stage: dispatch
  `agent-workflows:reviewer`, `:test-engineer` and `:github-worker` ([agent types][types]).
- Task records move to version 4 and gain members that an older `cw` cannot read, and its hook skips such a task instead of gating it:
  run the same `cw` on every host and hook that shares a task store. A `cw task` command on a record from a newer `cw`, or without
  `packs`, `test_rules` or `baseline_manifest`, exits 4 ([format](docs/development.md#task-record-format)).
- `cw task start` refuses a substantial plan without a check bound to a requirement: add one, and
  restart open tasks that have none ([start a task][start]).
- New tasks in Go modules get the `go.package-test/v2` rule in `cw task verify`: add direct `go test -race`
  checks covering each changed package, or opt out with `cw pack remove go` ([Go rule][start]).
- `cw task verify` needs a proven PASS for each mapped test and an independent test-engineer author
  for each changed Go test file: have the test engineer write them ([test authorship][authorship]).
- Nothing writes the hosts' model and effort settings any more: set them yourself in `settings.json`
  or `config.toml` ([local configuration](docs/workflow.md#local-configuration)).
- `cw task start` refuses a not-required checkpoint reason that claims no advisers while the task binds
  some: reword the reason or keep the checkpoints required ([checkpoints][checkpoints]).
- `cw task assign` needs `requirements` on a brief with a `contract` exactly when the plan has a
  per-test-credited `go test -json` check: declare them then, and only then ([briefs][assign]).
- A key variable such as `OPENAI_API_KEY` alone no longer binds an adviser: opt in with `advisers.enabled`, `cw keys set`
  or `--policy`; tasks already started keep their binding, and `cw doctor` names a key without an opt-in ([configure advisers](docs/advisers.md#configure-advisers)).
- Exit statuses follow one table: an incomplete `cw task verify`, a failed `cw task run` check and a partial `cw consult` exit 5,
  Graphify unavailable exits 4, `cw publish` unknown and mismatch exit 6 and 7: update scripts that test them ([exit statuses](docs/operations.md#exit-statuses)).

### Added

- Claude Code and Codex plugins in one marketplace, with the Go skills as the `cc-skills-golang`
  plugin ([install][install]).
- `cw install|uninstall --host codex` links or removes a checkout's skills and `AGENTS.md` block
  ([Codex](docs/operations.md#codex-install-and-uninstall)).
- Release archives for macOS and Linux (amd64, arm64) with `SHA256SUMS` ([get cw][getcw]).
- Seven worker agents, each with a role contract: spec-writer, architect, implementer, test-engineer,
  reviewer, github-worker and the opt-in adversarial-tester ([agents](docs/agents.md)).
- A Claude Code hook denies `git commit` and `git push` while an open substantial task has no passing
  `cw task verify`, and asks before large unrecorded changes ([hooks](docs/hooks.md)).
- The commit hook follows `cd` and `git -C`, including `cd "$PWD"`, `cd ~/proj` and `git -C "$HOME/proj"`; after `cd $OTHER`, `cd -`,
  `popd` or another change it cannot follow, it checks the session's directory and asks instead of denying ([recognised commands](docs/hooks.md#recognised-commands)).
- A task record `cw` cannot read makes the commit hook ask and `cw publish` exit 3: remove or move its directory to clear it.
  So does a store the hook cannot locate or read ([records](docs/hooks.md#records-that-cannot-be-read)).
- Under Codex, which runs the plugin hooks once you trust them in `/hooks`, the commit hook denies where Claude Code asks, because Codex cannot ask,
  except about a record or store it cannot read, which goes ahead; the docs say how to set options there ([Codex](docs/hooks.md#codex)).
- The gate's [threat model](SECURITY.md#threat-model) and [known bypasses](docs/hooks.md#known-bypasses):
  it guards a cooperative agent, not against a hostile one.
- `cw publish` commits, pushes and opens or edits the pull request of a verified task
  ([publish][publish]).
- `cw task add-checks` adds covering `go test` checks after a task merges its base branch
  ([merging the base](docs/operations.md#merging-the-base-branch-during-a-task)).
- `cw task abandon` closes a task so it stops gating; the commit hook asks before it, and under Codex denies it,
  and gate messages say to ask you before abandoning ([abandoning a task](docs/hooks.md#abandoning-a-task)).
- Requirement traceability: handoffs map a test engineer's `requirements` to tests, and `verify`
  judges each mapped test ([handoffs][assign]).
- `cw task trace`, `cw task authorship` and `cw task timeline` report requirement coverage, test
  authorship and wall time, read-only ([diagnostics][diag]).
- Review findings carry a `severity`, and `verify` lists rejected `blocker` and `major` findings
  ([acceptance][accept]).
- Language packs: the built-in Go pack, on by default, and `cw pack list|add|remove` ([packs][start]). The [packs](docs/packs.md) page
  documents `cw pack` and `pack.yaml`, with the steps and a proposal template for adding a language pack.
- `cw keys set|unset|status` manages adviser keys, never from the command line; disable an adviser in
  `preferences.json` ([adviser keys](docs/operations.md#adviser-keys)).
- `cw metrics import --append` keeps a cost history that outlives transcripts, and `cw metrics history`
  reports it ([cost history](docs/operations.md#cost-history)).
- The `codebase-design` [building blocks](skills/codebase-design/references/building-blocks.md)
  reference, which the architect, implementer and test-engineer contracts apply.
- Eval suites for six of the agents, checked offline by `go test ./evals/...` and run weekly in CI
  ([evals](evals/README.md#what-ci-runs)).
- `LICENSE` (MIT), `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `SECURITY.md`, issue and pull request
  templates, CODEOWNERS, Dependabot and folder guides ([contributing](CONTRIBUTING.md)).
- `cw hook git pre-commit` and `cw hook git pre-push` run the commit gate from git's own hooks, which also see aliases and scripts:
  a gating task exits 3, and each ask is a warning; `CW_GATE=off` or `--no-verify` skips it ([git hooks](docs/hooks.md#git-hooks)).
- A check can name variables such as `DB_PASSWORD` in `env_passthrough` to receive them, where `cw` otherwise drops names with `KEY`, `TOKEN`,
  `SECRET`, `PASSWORD` or `CREDENTIAL`; `cw task verify` lists each as a limitation ([check environment](docs/operations.md#what-a-check-runs-with)).
- `cw gate install`, `uninstall` and `status` write, remove and report a repository's git pre-commit and pre-push hooks that run that gate;
  they never change a hook cw did not write, or a hooks directory other repositories use without `--shared` ([installing](docs/hooks.md#installing-the-git-hooks)).
- The session-start context gains one line when the plugin and `cw` versions differ, naming the one to update, and one
  when Claude Code is older than 2.1.292; a `cw` built between tags is not compared ([session start](docs/hooks.md#session-start)).
- While a substantial task is open in a repository without git's hooks, the session-start context suggests `cw gate install` once
  for that task and `cw doctor` reports `git_hooks` as missing; it never installs them ([reminders](docs/hooks.md#installing-the-git-hooks)).

### Changed

- Substantial tasks review each stage (spec, design, tests, code), send findings back to its author, cap
  revision rounds ([review stages][stages]) and fix a wrong contract by [amendment][amend].
- Code review checks the source against the contract, `design.md` and the coverage profile, with a
  failing test for each behaviour finding ([code review][review]).
- The reviewer and test engineer start without CLAUDE.md files (Claude Code 2.1.271 or newer); briefs
  quote the rules they need ([agent types][types]).
- In a repository with `docs/decisions/`, the architect also writes an ADR for a choice that is costly
  to reverse ([architect](skills/task-orchestration/references/contracts/architect.md#required-outputs)).
- The always-on `global/AGENTS.md` block and the `task-orchestration` skill core keep only the decision
  flow; the rules live in references ([skill](skills/task-orchestration/SKILL.md)).
- `cw` no longer reads its model policy or configuration from an installed release
  ([policy](docs/operations.md#model-policy), [paths](docs/operations.md#paths)).
- Diagrams are archify specs with committed SVGs; Mermaid is gone ([diagrams](CONTRIBUTING.md#diagrams)).
- `cw` runs on macOS and Linux; Windows is not supported ([get cw][getcw]).
- CI runs gofmt, vet and tests on Linux and macOS, plus golangci-lint and govulncheck for both
  ([CI](docs/development.md#continuous-integration)); releases come only from pushed `v*` tags and
  build with go1.27.4, which fixes 11 reachable stdlib vulnerabilities ([releases](docs/development.md#releases)).
- Both plugins and the marketplace carry version `3.0.0-rc.2`; install `cw` from the `v3.0.0-rc.2` release instead of `@main` ([get cw][getcw]).
- The README's first task is a substantial one, with a line for small changes and the planned, unmeasured per-task
  cost caps ([first task](README.md#your-first-task)).
- Without `cw` on `PATH`, the session-start hook prints its install command, and `global/AGENTS.md` and the
  task-orchestration skill say to stop and give it ([session start](docs/hooks.md#session-start)).
- The overview diagram ends with `cw publish`, with the GitHub worker as fallback, and shows the opt-in adversarial
  tester ([how it works](README.md#how-it-works)).
- Security fixes go to the newest v3 release, release candidates included ([supported versions](SECURITY.md#supported-versions)).
- The commit gate's deny names at most three open tasks, each directory once, and counts the rest with their task store,
  so it stays short with many open tasks; the Codex deny and git hook stderr follow ([which records gate](docs/hooks.md#which-records-gate)).
- The commit gate's rules live in `internal/gate`, so contributors unit-test them with a fake git, clock and task store
  ([development](docs/development.md)).
- The credential store, cost history and Graphify cache share one `flock` lock in `internal/fslock`,
  released when its holder dies; new locks reuse it ([development](docs/development.md)).
- One table compares the Codex install shapes and recommends the plugin with its hooks trusted; untrusted, it gives
  skills only, and `cw install --host codex` adds archify but no commit hook ([Codex](docs/operations.md#codex)).
- Task checks run on the shared `internal/proc` runner through `taskflow.RunCheck` (was `taskflow.Run`), so contributors change
  a check's program, environment or bounds in one function, `checkSpec`; check records are unchanged ([development](docs/development.md)).

### Fixed

- Source capture reports a Git probe that does not finish cleanly instead of treating the repository
  as a plain folder ([diagnostics][diag]).
- `cw metrics import --composition` no longer slows down on long or deeply nested Bash commands
  ([diagnostics][diag]).
- `cw consult` reports a failed close of its output record instead of exiting 0 with a record that
  may be incomplete.
- `THIRD_PARTY.md` lists go-toml v2.4.3, the version `cw` links; a test keeps the table in step with `go.mod`.
- Worker agents can no longer dispatch subagents or run workflows, and the README states the minimum Claude Code version:
  2.1.292, for per-dispatch `effort`; below 2.1.271 `omitClaudeMd` is also ignored silently ([agents](docs/agents.md)).
- A relative `XDG_STATE_HOME` is refused with exit status 1, and the commit hook asks, instead of joining it to the working
  directory, which moved the task store, and the gate with it, on every `cd`; set an absolute path ([paths](docs/operations.md#paths)).
- `cw doctor --live` probes and Git inspection kill their process group until it is empty, as checks already did, so a child forked
  as the kill lands cannot outlive them (one kill can miss it on macOS) ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).
- The `go list` runs behind `cw task verify`'s Go facts kill their process group when they end, so a child of `go` or a `go` wrapper
  no longer outlives them or their time budget; `cw publish` and `cw graphify` share that runner ([development](docs/development.md)).
- A killed `cw` no longer leaves its task, or the task's `cw publish`, locked, and a second task writer waits up to 10 s instead of failing;
  do not mix `cw` versions on one task ([task locks](docs/operations.md#task-locks)).
- A check named `go` runs only a `go` built from `cmd/go`, with its own GOROOT, through shims and toolchain switches, and records it, and
  `cw task verify` reads its Go facts from such a `go`: a fake `go` on `PATH` fails the check (`untrusted_toolchain`) instead of passing
  and cannot waive `-race`; put the real `go` first on `PATH` ([which go](docs/operations.md#which-go-a-check-runs)).

### Removed

- The v2 installer: `install.sh`, the setup wizard, `cw status|update|rollback|recover` and the
  release snapshots ([install][install]).
- `cwdev check`, `cwdev release` and `cwdev sandbox` ([development](docs/development.md)).
- The `db-postgres` third-party skill, whose license was ambiguous
  ([license rule](CONTRIBUTING.md#proposing-or-changing-a-skill)).
- Archify's rendered HTML examples and the Go pack's `EVALUATIONS.md`, 13 MB of the plugin download;
  `archify examples --output-dir <dir>` renders the examples ([third-party notices](THIRD_PARTY.md)).
- The Go rule `go.package-test/v1` is gone: `cw` refuses a task that binds it, which only development builds before
  v3.0.0-rc.1 started, and an open one gates until moved; start a new task ([format](docs/development.md#task-record-format)).

[install]: docs/operations.md#install
[getcw]: docs/operations.md#get-cw
[types]: skills/task-orchestration/references/hosts.md#worker-agent-types
[start]: skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task
[authorship]: skills/task-orchestration/references/worker-handoffs.md#test-rules-and-test-authorship
[publish]: skills/task-orchestration/references/github-worker.md#publish-with-cw-publish
[assign]: skills/task-orchestration/references/worker-handoffs.md#assign-before-dispatch
[diag]: docs/operations.md#global-project-diagnostics-and-evidence
[accept]: skills/task-orchestration/references/worker-handoffs.md#return-and-accept-evidence
[stages]: skills/task-orchestration/references/worker-handoffs.md#review-stages
[review]: skills/task-orchestration/references/contracts/code-review.md#uncovered-changed-blocks
[amend]: skills/task-orchestration/references/contracts/README.md#contract-amendment
[checkpoints]: skills/task-orchestration/references/consultation-helper.md#task-checkpoints

## [2.0.96] - 2026-11-07

Last release published automatically from main.

[Unreleased]: https://github.com/belevtsev/agent-workflows/compare/v3.0.0-rc.2...HEAD
[3.0.0-rc.2]: https://github.com/belevtsev/agent-workflows/compare/v2.0.96...v3.0.0-rc.2
[2.0.96]: https://github.com/belevtsev/agent-workflows/releases/tag/v2.0.96
