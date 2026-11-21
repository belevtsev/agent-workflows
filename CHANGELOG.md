# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Breaking

- Install the opt-in [`kitsu-team` plugin](docs/release-install.md#add-role-coordination) to keep the role agents and the managed workflow in your host. The CLI commands are unchanged.
- Start managed work with the `managed` skill of `kitsu-team`. On Claude Code, the role agents are now named `kitsu-team:<role>`.
- Register a [trusted runner](docs/workflow-runner.md) for `kitsu task run`. Continue a local-launch preview record by opening a new task with the original request and assertion.

### Added

- Open, verify and commit a daily task or bug ticket with [one `kitsu task do` command](docs/task-harness.md#do-a-daily-task-in-one-command).

- Show actionable [Jev story review results](docs/advisers.md#review-feature-stories-at-intake-with-jev) in brief task status.

- Give reviewers the original request, acceptance criteria and current checks in a [shorter feature handoff](docs/task-harness.md#choose-independent-review).

- Opt in to [verification before independent review](docs/task-harness.md#verify-before-review) for eligible feature deliveries.

- Opt in to [Jev story review at feature intake](docs/advisers.md#review-feature-stories-at-intake-with-jev) with a local approval.

- Resume saved assertions and feature checks from the [daily task handoff](docs/task-harness.md).

- Save feature acceptance criteria and include their observed checks in a [verification receipt](docs/task-harness.md#deliver-a-feature).
- Start a [failed PR check repair](docs/task-harness.md#repair-a-failed-pr-check) with an approved local check. Finish verification, commit, and receipt in one command.
- Approve a private reproduction command for a [failed PR check repair](docs/task-harness.md#repair-a-failed-pr-check) with `kitsu check trust --ci`.
- Assign each specialist to Codex or Claude Code and run or resume tasks with the [configured workflow](docs/configured-workflow.md).
- Choose a default host during [installation](docs/release-install.md), then customize repository role assignments with `kitsu role init` and `kitsu role set`.
- Follow a task from chat to its saved state and a bug ticket to its verified commit in the [illustrated task guide](docs/task-harness.md#how-a-daily-task-runs).
- Review and approve repository check commands for agent sessions with [`kitsu check trust`](docs/cli.md#approve-repository-checks).
- Required daily reviews record the native Codex or Claude Code worker and the exact candidate it inspected.
- Share saved patch or commit verification with [`kitsu task receipt ID --markdown`](docs/task-harness.md#share-a-verification-receipt).
- A shorter task skill supports the [same-worktree handoff preview](docs/verified-handoff-preview.md) across Codex and Claude Code.
- Direct ticket imports connect only to public addresses. Use a host connector to [supply a private ticket](docs/task-harness.md#deliver-a-bug-ticket).

### Changed

- State the [measured token cost](docs/benchmarks.md#what-an-agent-spends) of the role workflow before a new `kitsu task run` and in the `managed` skill.
- Deliver a verified candidate with a plain `git commit`. The [receipt](docs/task-harness.md#do-a-daily-task-in-one-command) names that commit without another Kitsu command.

- Deny an agent's shell the host's review notices, a terminal of its own for approvals and writes to the task ledger. [What the guard denies](docs/operations.md#kitsu-guard)

- Name added authority, replaced assertions, revised briefs and supplied PR checks in the [verification receipt](docs/task-harness.md#share-a-verification-receipt).

- State what a host-observed review and a terminal approval prove: evidence, not authentication. [A guardrail, not a sandbox](SECURITY.md#a-guardrail-not-a-sandbox)

- Review a daily task with a [cheaper default reviewer](docs/task-harness.md#choose-independent-review). A reviewer that you set in `kitsu-roles.toml` still applies.

- Load a shorter task skill and session notice in every session. The [step-by-step commands](skills/task/references/steps.md) are read only when a task needs them.

### Fixed

- A task no longer stays blocked when a commit hook refuses Kitsu's commit and you then commit a corrected tree with Git.
- [Failed PR check intake](docs/task-harness.md#repair-a-failed-pr-check) also recognizes failures reported through commit statuses by external CI.
- [Failed PR repairs](docs/task-harness.md#repair-a-failed-pr-check) now pause when the checkout moves off the reproduced PR head before delivery.
- [Supervised imports](docs/workflow-runner.md) preserve returned bytes across Git line-ending settings. The guard gives specific diagnostics for aliases of protected files.
- [Installers](docs/release-install.md) use the latest release for CLI-only installs and preserve the personal host unless explicitly replaced.
- The [pre-push hook](docs/hooks.md#pre-push) rejects malformed ref updates and records empty input as a no-op.

- [Release installers](docs/release-install.md#install-from-a-release) stage the shell script before running. The release workflow tests and attests the exact bundle before publishing.

- Release archives include the [license for Go networking and HTML dependencies](THIRD_PARTY.md).

- Require initialized submodules for new [check evidence](docs/evidence.md#the-same-content). Rerun checks once on trees that contain submodules after upgrading.

- Help for `check add` and `task test` now puts Kitsu options before `--`. The [verification example](docs/cli-reference.md#kitsu-check-add) shows how to save a required check.
- Completed commits and publication confirmations remain historical after a branch moves. Formatter hooks that change a commit require fresh verification and preserve concurrent staging.
- Daily tasks keep their opening check policy and require repair authority before testing a changed verify-only candidate after a failure.
- Git commands allow output more time to drain on busy runners, including when reading the index.

## [0.0.2] - 2026-11-18

Install the CLI and matching Codex or Claude Code plugin with one command on macOS, Linux or Windows. Both come from the same public release and are verified against its checksum manifest. [Installation commands](docs/release-install.md)

- Start a bug repair from a Markdown ticket or public link and produce a delivery receipt linked to the verified commit. [Ticket walkthrough](docs/task-harness.md#deliver-a-bug-ticket)

### Added

- A [daily coding harness](docs/task-harness.md) supports outcome-scoped work, independent review, repair and resumable local verification.
  Gate detachment preserves check history by default. Use `--purge` for explicit removal.

- A [response-cycle comparison](docs/benchmarks/response-cycles-paid-report.md) records −11.4% compact tokens in one completed pair;
  all coordinator/worker sessions are counted, protected checks pass, and general quality or billing savings remain unproven.

- Opt-in [native launch views](docs/compact-response-cycles.md) retain mandatory worker fields and a read-only original full dispatch;
  QE packets add bounded command recipes and scoped optional-tool observations, with existing quality and gate authority.

- A [Codex follow-up](docs/benchmarks/workflow-insights-paid-report.md) records complete coordinator and worker usage;
  both attempts pass protected checks, while compact uses 21.1% more captured tokens in this pair and remains opt-in.

- Optional [workflow insights](docs/workflow-insights.md) join captured usage, managed intervals and repeated failures;
  source-linked notes and task budget metadata preserve unknowns without enforcing caps or authorizing paid work.

- Opt-in [task behavioral verification](docs/verification.md) prioritizes mapped checks and records source-bound attempts;
  final gate checks and current local evidence still determine readiness.

- [Offline verification](docs/verification.md) adds historical defect calibration, generated evidence-equivalence checks
  and bounded Go fuzz targets without model calls; compact remains opt-in.

- A [revised Codex comparison](docs/benchmarks/compact-delivery-paid-report.md) captures every solver worker and protected quality checks;
  compact uses 5.5% fewer total tokens in one completed pair, with higher uncached input and no general quality or billing claim.

### Changed

- Document [feature delivery](docs/task-harness.md#deliver-a-feature), checkout review order, resumable review, and Jev intake for both hosts.

- [Compact handoffs](docs/coordinator-overhead.md) use shorter native prompts and worker-focused guidance, ordinary quiet checks,
  and minimal returns; [offline replay](docs/benchmarks/compact-delivery-report.md) checks fidelity without paid model calls.

### Fixed

- [Task verification](docs/task-harness.md#retry-verification) detects staging changes and rechecks the staged candidate before consuming an earlier commit ticket.

- [Checkout discovery](docs/operations.md#where-kitsu-keeps-its-records) preserves newlines in Git paths and reports errors from damaged repository configuration.

- [QE tool observations](docs/compact-response-cycles.md#use-the-qe-packets-recipes) stay unknown when the current directory is removed or replaced,
  including when macOS reports its stale path.

## [0.0.1] - 2026-11-17

Initial Kitsu release after the release reset. Versioning restarts at `0.0.1`;
earlier `cw` releases remain documented below as historical source revisions.

### Breaking

- `cw` and Agent Workflows become Kitsu: use `kitsu`, rename configurations, and replace the old hooks and plugins.
  No aliases or old evidence are imported; follow [migration](docs/migration.md) and rerun the checks.

### Added

- [Coordinator state](docs/coordinator-state.md) adds opt-in bounded `task show --compact` snapshots and `--since` section deltas,
  with original references, current check evidence, policy changes and completion blockers; ordinary task views stay unchanged.

- [Coordinator handoffs](docs/coordinator-overhead.md) add opt-in generated native dispatch and bounded untrusted worker returns;
  managed guidance avoids duplicate bookkeeping, and corrected offline replays account for full assignment delivery.

- Opt-in [typed context and worker handoffs](docs/compact-context.md) add native reads/searches, saved original briefs,
  and ordered `task prepare`/`finish` write checks without launching models or replacing review and gate evidence.
- An opt-in [Codex pilot harness](docs/codex-compact-pilot.md) freezes paired real-task experiments and accounts for every solver session;
  ordinary tests skip paid execution, and independent grading separates functional results from workflow compliance.
- Opt-in [Go context collection](docs/compact-context.md) batches source reads, prepares fresh worker briefs and bounds check output,
  retaining expandable evidence; offline comparisons and source-linked research distinguish byte reductions from unmeasured token savings.

- [Agent onboarding](docs/cli.md#check-setup-and-start-a-task) checks local host, plugin, skill, and hook setup;
  `kitsu task start` launches the selected host or prepares a handoff with explicit trust and runtime limits.
- A grouped [CLI](docs/cli.md) adds complete help, private host preferences, explicit terminal editors,
  skill actions, and completion for four shells while preserving existing commands quietly.
- Select [existing skills](docs/skills.md) per role from local repository, personal, and plugin sources,
  with layered bindings, source hashes, and portable selection receipts.
- Optional, experimental [Jev and SubQ Snap advisers](docs/advisers.md#built-in-decision-advisers) provide typed decisions and separate token usage.
  None are enabled by default; definitions, repository role access, and required checkpoints need explicit opt-in.
- Shell and PowerShell [installers](docs/release-install.md) select your platform, verify checksums, and set up user `PATH`.
  Install from release or CI artifacts without a clone or Go; rerun to update, or choose a version and directory.
- Set or reset a role's model and reasoning from the CLI with [`kitsu roles set` and `reset`](docs/roles.md#changing-a-model),
  preserving other repository settings without editing TOML manually.
- Configurable [advisers](docs/advisers.md) answer role questions and compare choices, with task bindings,
  required checkpoints, recorded dispositions, and a role/model/reasoning/access table.
- A Pomeranian mascot and matching README banner introduce Kitsu's [visual identity](docs/brand/README.md).
- Three diagrams: the three parts of `kitsu`, how the gate decides about a commit or a push, and the seven roles on the two paths
  of a task. The README, [hooks](docs/hooks.md) and [the task workflow](docs/workflow.md) show them.

### Changed

- The [installation guide](docs/release-install.md) leads with one command for Linux/macOS and one for Windows;
  installers and plugin manifests use `v0.0.1`, while CI artifacts remain a contributor option.

- [Compact mode](docs/compact-context.md) now prefers native short reads and scoped searches, reserving collection for noisy retrieval
  and checks; a [separate evaluation](docs/benchmarks/selective-compact-report.md) counts expansions, retries and bounded canary usage.
- [Role defaults](docs/roles.md#the-roles-at-a-glance) use different models and reasoning efforts for lookup,
  specification, architecture, tests, implementation, review, and Git work on both supported hosts.
- Lead with [coding-agent coordination](docs/workflow.md) in the documentation, plugin descriptions and CLI help; the Git gate supports that workflow.

### Fixed

- [Installation docs](docs/release-install.md) distinguish unpublished builds after the release reset;
  historical changelog links use source commits instead of deleted tags and releases.

- [Pilot validation](docs/codex-compact-pilot.md) accepts clean Windows rollout/copied-home paths and trusted repository aliases,
  retaining containment, attestation and candidate-tree checks while rejecting duplicate home destinations.

- [Path completion](docs/cli.md#enable-shell-completion) follows `~/` and nested home directories correctly,
  with native directory separators and regression coverage on all supported platforms.
- The [installer](docs/release-install.md#path-and-updates) makes Kitsu available in new Bash login shells
  on Linux even when no user login profile exists yet.
- Marked files are compared with Git's type, executable-mode and symbolic-link semantics, so a hidden mode change
  cannot produce passing evidence and an unchanged link is accepted ([evidence](docs/evidence.md)).
- Shipped changes use `kitsu roles check spec-writer --finalize`, limited to the task's capabilities in the baseline
  snapshot, so current specifications can be synchronized within the role's limit ([workflow](docs/workflow.md#the-archive)).
- Duplicate scenario names across active requirements are rejected, so one test mapping cannot cover distinct scenarios
  accidentally ([workflow](docs/workflow.md#the-rules)). Rename the duplicate and update its test-table row.
- [The task workflow](docs/workflow.md) said that no part of the workflow had run with a model. A model ran the skill `task` in
  three sessions and started roles in two of them, as [roles](docs/roles.md#what-is-not-known) already said.

### Removed

- The folder `_quarantine/`, which held the files of the previous workflow as reading material. Each file is kept
  unchanged on the branch `workflow-v3`, at the same path without `_quarantine/` in front.

## [3.0.0-beta.1] - 2026-11-15

Historical `cw` release. Its GitHub release, tag and assets were removed on 17 November 2026;
the links below retain the original source revision.

### Breaking

- `cw` is rebuilt as a gate that any git repository can use. Checks are declared in a committed `cw.toml`, and a commit or a
  push is allowed only when every declared check has a recorded passing run on exactly that content.
- The commands are `cw init`, `add`, `run`, `status`, `stats`, `context`, `hook`, `guard`, `uninstall`, `roles`, `change`,
  `tokens` and `version` ([operations](docs/operations.md)). Every other command is gone: `cw task`, `publish`, `gate`,
  `consult`, `keys`, `pack`, `doctor`, `install`, `graphify` and the rest.
- The skills, the agents, the language packs and the vendored tools left this branch. They are kept on the branch `workflow-v3`.
- Records are kept under `cw/` in the repository's git directory. The task store under the home directory is no longer read.

### Added

- Seven roles for a coding agent, each with its model and what it may write: `cw roles`, `cw roles text` and
  `cw roles check`; the plugin ships them as agents and the skill `task` ([roles](docs/roles.md)).
- `cw change status` prints the stage of a task's record under `openspec/changes/<task>/`, and `cw change check` checks
  the rules its files must obey ([the task workflow](docs/workflow.md)).
- `cw tokens` reports what a task used by role and stage, and the share the workflow itself took; a count that
  cannot be read is `unknown`, never zero ([tokens](docs/tokens.md)).
- The skill `cw` tells a coding agent how to work in a gated repository: the loop for any change, what each denial
  means, and seven examples that a test replays against `cw` ([skill](https://github.com/belevtsev/kitsu/blob/511ceb11c1946c4185c5a93c0c0a9e79613bdc3d/skills/cw/SKILL.md)).
- `cw guard` reads a shell command as words. It asks before `git commit -n`, a setting of `core.hooksPath` and an edit of
  a hook file or of a record, and no longer before a command that only mentions `--no-verify`
  ([operations](https://github.com/belevtsev/kitsu/blob/511ceb11c1946c4185c5a93c0c0a9e79613bdc3d/docs/operations.md#cw-guard)).
- `cw tokens record` reads the running totals that Codex writes; the first reader found no count in any real Codex
  record. Both readers were compared with a separate count on real records
  ([tokens](docs/tokens.md#checked-against-real-records)).
- A review of what users report about other tools, with sources and what `cw` did about each finding
  ([what other tools taught us](docs/other-tools.md)).
- Code for Windows, and release archives that need no Go toolchain. CI runs the tests on Linux, macOS and Windows,
  builds the archives from every commit, and follows the install instruction of the README with them on the three
  systems, on Windows in both PowerShells ([development](docs/development.md#continuous-integration)).
- `cw <command> --help` explains every argument and option of the command.
- First measurements of what `cw` costs: the time of a commit on the three systems, the time of each command against
  the size of a repository, and the tokens of an agent with the gate and with the roles on two small tasks, with the
  scripts that repeat them ([benchmarks](docs/benchmarks.md)).
- The text of a release is the section of this changelog ([development](docs/development.md#releases)).

### Changed

- The hook files and every command of the plugin look for `cw` only in the entries of `PATH` that are absolute paths;
  an empty or relative entry is not searched. Run `cw init` again in a clone to get the new hook file.
- The plugin's commands find `git` the same way, and take a `cw` that cannot be run as not installed. Under Codex on
  Windows they go to the root of the drive and then to the Windows folder first, so a relative entry of `PATH` leads
  there and not into the repository ([hooks](docs/hooks.md#hooks-of-the-agent-plugin)).
- The plugin's commands always exit 0, so an older `cw` blocks no tool call ([hooks](docs/hooks.md)).
- A hook counts as calling `cw` only on a line that is no comment. In a `core.hooksPath` directory `cw uninstall`
  removes only a file that is exactly its own script, and it prints how many hooks it removed.
- A submodule entry counts in the content key of every check with `paths`. A pattern that names a folder is an invalid
  declaration. A submodule that is not checked out but holds files, or that holds a path git is told to overlook, is
  refused ([evidence](docs/evidence.md)).
- A push of `refs/notes/` is not judged, and a tag that a branch of the same push contains adds nothing. A tree without
  `cw.toml` is denied with a link to what to do ([operations](https://github.com/belevtsev/kitsu/blob/511ceb11c1946c4185c5a93c0c0a9e79613bdc3d/docs/operations.md#a-tree-without-cwtoml)).
- The guard compares files as the file system sees them, also under `core.hooksPath`. It asks before `cw uninstall`,
  `cw init --observe`, an edit of the git configuration and a patch given to the shell, reads commands behind
  `timeout`, `exec`, `nohup` and `eval`, and matches the `PowerShell` tool ([operations](https://github.com/belevtsev/kitsu/blob/511ceb11c1946c4185c5a93c0c0a9e79613bdc3d/docs/operations.md#cw-guard)).
- A denial says to fix what the log shows when a check failed, names a last run that decided nothing, and names
  `git add -A` and `git stash push --keep-index --include-untracked`. A failed `cw run` ends with one `not verified` line,
  and `source_changed` says what to do about a file a check writes.
- `cw status` and `cw context` say first when no hook of `cw` judges commits in a clone. `cw init` prints `next: cw run`
  where checks are declared. `cw context --host` reads the `cwd` of the host's notice.
- `cw add` refuses a program that is one word with a space and no such file, names `--` when an option is unknown, says
  that it only appends, and writes `cw.toml` whole and synced. The ids `con`, `prn`, `aux`, `nul`, `com0`-`com9` and
  `lpt0`-`lpt9` are refused.
- git runs with `GIT_NO_LAZY_FETCH=1` and without its file system monitor. Logs, records and decisions are created
  `0600`, the folders of the store `0700`.
- On Windows Ctrl+C is recorded as `interrupted`. A check's program named without a folder is found on `PATH` only,
  one given as a path is read from the repository root, and the program is started by the full path `cw` found.
- Under Codex on Windows the start of a session prints the line that says `cw` is not installed.
- `cw uninstall` prints the line of a hook file that holds the call of `cw`, as the file holds it.
- `cw tokens record` creates its folders `0700`.
- The role texts say who owns an existing test, how a wrong test goes back, and that a task is ticked after its review;
  `cw change check` runs after each role that writes the record, and refuses a test path that leaves the repository.
- `cw tokens record` reads again the starts whose record grew, carries a Codex total over a reset, ignores an internal
  agent without a type, and uses the `cwd` of the notice ([tokens](docs/tokens.md)).

## [workflow-v3] - not released

The entries below describe the previous workflow. It was never released from this state and lives on the branch
`workflow-v3`. Links to its documents are plain text here, because those documents are not on this branch.

### Breaking

- Pass `--task DIR` to every `cw task` command that takes it: the working directory is no longer used as the task,
  and a command without `--task` exits 2 with `--task DIR is required` (exit statuses).
- The Go rule credits a `go test` run only under the `GOFLAGS`, `GOOS` and `GOARCH` its go command reports with `go env`, and task
  records move to version 5: rerun Go checks recorded before, and upgrade every `cw` sharing the store (which go).
- With cgo on, the Go rule credits a `go test` run only when `cw` verified its C, C++ and Fortran compilers, swig, assembler, linker,
  `CGO_*FLAGS` and loader variables; rerun Go checks recorded before this (the C toolchain).
- `cw consult` sends a task-bound request only when its task, not abandoned, holds that task context in the store the hook reads,
  so a request cannot name advisers its task did not bind: for a `--store DIR` task, set `CW_TASK_STORE=DIR` (consultation).
- `cw task` `start`, `run`, `assess`, `assign`, `handoff`, `accept`, `consult`, `verify` and `add-checks` print their one-line summary by default:
  a script that reads the whole record passes `--full`; `--summary` is still accepted (diagnostics).
- `cw task show` prints the task's state and field names (`--full` for the record), `cw task timeline` the last 20 entries (`--last 0` for all),
  and the `verify` summary joins like problems and lists at most 20 (diagnostics).
- `cw task` summaries and the commit gate's deny reason name the task by `task_id` instead of its directory, and a log path is relative
  to the task directory: pass the ID to `--task`; `--full` keeps the record (diagnostics).
- Codex runs the plugin's new `hooks/codex-hooks.json` (`cw hook … --host codex`) instead of `hooks/hooks.json`: trust the hooks again in
  `/hooks` after updating, or Codex runs none; a given `--host` decides the host over the hook input (Codex hooks).
- `--dry-run` is a flag of `cw consult`, `cw pack add|remove` and `cw task init` only, after the command name: drop it from any other command,
  which now exits 2 (`task show --dry-run` did not); so does an extra argument on every command, `cw hook` included (paths).
- The commit gate counts a task record whose latest revision is dated after the current time as just written, where it skipped it;
  `cw task abandon` is the way out of a record with a wrong far-future date (which records gate).

- While a task that passed `cw task verify` is open, both commit gates deny a `git commit` recording staged bytes or a mode that differ
  from the verified working tree for a path it changes, and ask about one a git filter staged (which source counts).

### Added

- A ten-minute quick start takes one task by hand from `cw task init` to a verified commit, and a test runs
  its commands, so the page stays true; README "Your first task" now links it.
- `cw task init --title T` writes a starter plan that `cw task start` accepts unedited: profile, one requirement and, in a Go module,
  the Go pack's race command as its check; it prints text by default and JSON with `--json` (starter plan).
- `cw task init --title T` writes a starter plan that `cw task start` accepts unedited: profile, one requirement and, in a Go module,
  the Go pack's race command as its check; it prints text by default and JSON with `--json` (starter plan).
- `cw task schema <command>` prints the JSON Schema of a task command's JSON input, and the `--help` of each task command that reads
  JSON, and of `run` and `verify`, ends with an example (diagnostics).
- `cw task start` fixes a profile (`lite` by default, `standard` or `strict`) in the task record and prints it as `profile` in its summary, with
  stages, round caps and a provisional dollar budget a plan's `budget` may lower but not raise; `cw task assign` enforces the round caps
  and fix rounds, and it and `cw task run` stop new work at the budget's stop (profiles).
- `cw task assign` refuses a round past its stage's cap with exit 4 and no revision, unless `--owner-approval "<the owner's words>"`
  records the owner's words on the assignment; the plugin hook asks the user to confirm them (round caps).
- `cw task assign` refuses a third fix round on a finding or requirement still open (exit 4); the owner decides, by words given with
  `--owner-approval escalation=…`, between one more round and a new approach, or disposes of the finding (fix rounds).
- At 80% of the task's estimated dollar budget, `cw task assign` and `cw task run` exit 4 and ask the owner, whose `cost-budget` words go on
  the task; the plugin hook samples Claude Code and Codex usage before `cw task` commands (budget stop).
- Every `cw task` command takes the task ID for `--task` as well as the directory, found in the default task store
  or in `$CW_TASK_STORE`, so a call need not repeat the absolute path (diagnostics).
- `cw gate install` also writes git's `pre-merge-commit` hook, so a merge commit without conflicts meets the gate as a commit does;
  run it again where an older `cw` wrote the hooks, which `cw doctor` and the session-start line then name (git hooks).
- Each check run records the digest of the source it ran at and a key of its go and `env_passthrough` configuration, keyed per task,
  and each worker result its source digest, so a later pass can be matched to unchanged bytes (run keys).
- `cw task assign` records the stage a brief answers and, for a later round, the findings or requirements it fixes or the assignment it repeats;
  under a profile it refuses an implementation or review brief without them, naming the member (assign).
- A passed check run stays credited across commits and merges that keep the source bytes and modes, go command, `GOFLAGS` and run key, and
  `cw task run` then reuses it without running, except a `git` check; `--force` or `CW_NO_REUSE=1` runs it (reuse).
- `cw task usage` samples a session's tokens into the task record with dollars estimated from a dated built-in price table (or `--prices`);
  only requests since the task's start count, task usage sums each session's latest sample, and what cannot be priced stays unknown, never 0 (cost budget).
- Under a profile `cw task handoff` caps a result at 6 KiB, its summary at 1,200 bytes and each finding, reach-run or `uncovered` text
  at 400, and stores the report its `report` member names as `report-<assignment>.md`, checked on every load (handoff caps).
- `cw task brief --stage S` prints, in at most 4 KiB, what a fresh worker of a stage starts from, and `cw task assign` refuses an `agent_id`
  that worked on another stage unless the brief `repeats` an assignment (stage briefs).
- `cw task receipt` prints, in at most 4 KiB, a task's rounds, owner approvals, estimated dollars and tokens (or unknown, never 0),
  checks, findings and wall time; `verify`, `abandon` and a budget stop print it on stderr (task receipt).
- `cw task list` prints this repository's tasks with their state (open, verified, abandoned, stale or unreadable), as text or with `--json`;
  the commit gate's reasons point at it for the tasks past the three they name (listing tasks).
- Evidence reports the first full local run of the six agent eval suites (29 cases): four suites at or above the
  0.8 pass floor, `architect` and `adversarial-tester` one case below it, and why each grader failed; `evals/README.md` gains the Linux
  host steps a run needs.

### Changed

- A substantial task runs the `lite` flow by default: implementer and test engineer in parallel, test review, code review; `standard`
  adds the design stage and `strict` the spec stage (profiles); reviewers skip style.
- "Substantial task" is defined only in the working conventions; a task that is not substantial dispatches no workers, and the agent
  checks its own diff; a new glossary names each term and role once.
- The orchestration skill is cut a second time, from 187 KB to 144 KB: every worker contract stands alone, the two host pages merge
  into `hosts.md`, the task presets go, and each `cw publish` status moves to the docs (publishing).
- The orchestration skill loads less: publication splits into `cw publish` and its fallback worker, the host mapping into a page per host,
  and the multilingual eval corpus moves out of the installed skill to `evals/multilingual/` (host mapping).
- The test-engineer and code-review contracts hold worker rules only (each under 8 KB), so a dispatched worker reads less; the coordinator's
  part moves to dispatching reviews and tests.
- The orchestration skill's workflow reference shrinks from 46 KB to 10 KB: base-branch merges and `cw metrics` get their own references,
  and the Go rule and test evidence rules move to operations.
- The worker-handoffs reference is cut from 36 KB to 10 KB and states the review cap as three rules and the round budget as four;
  what `cw task assign`, `handoff` and `bundle` check moves to worker records.
- The coordinator reads adviser guidance only when a task binds advisers; checkpoints and adjudication alone need the
  checkpoint helper, now under 10 KB (advisers).
- A test holds each `*.md` file of the orchestration skill, and their sum, to a byte cap that only goes down, toward 10 KB a file and
  90 KB in all; a new file needs a cap (repository content checks).
- The 90,000-byte budget of the orchestration skill counts what a default `lite` task loads (about 76 KB): the other references are
  listed with the condition that loads each, and the folder total only goes down (content checks).
- CI gives each package 15 minutes on macOS, where `cmd/cw` takes over eight; go test's default of 10 failed it
  (CI).
- CI tests the Go packages in four shards per system (`internal/taskflow`; `cmd/cw`; `internal/cli` with `internal/langpack`; the rest), so the Linux race run
  and the macOS run finish sooner; the required checks `Native Go / ubuntu-latest` and `Native Go / macos-latest` keep their names and pass
  only when every shard passes (continuous integration).
- One host requirements table lists the platform, Claude Code, Codex, Go and git versions; the docs name model aliases and profiles
  and link the model policies instead of model releases, and drop unmeasured token counts (host requirements).
- `pack.yaml` keeps only the fields `cw` reads (`commands.race` among them), dropping `tests`, `skills`, `review` and the
  other unread slots, and `cw pack list` prints each pack's declared fields, now with `commands` (pack.yaml).
- `cw --help` lists the core commands apart from the experimental ones (`consult`, `graphify`, `keys`, `metrics`), which semver
  does not cover; one page states what each version number promises (stability).
- `cw --help` no longer lists `cw graphify`, which needs a Python runtime `cw` does not install; the command and
  `cw graphify --help` still work (stability).
- `cw doctor --summary` lists under `problems`, and turns `attention` for, only what blocks the workflow (git, `cw` on `PATH`, the skills);
  optional tools are counted in `optional_missing`, and `--all` lists them (diagnostics).
- The `pre-merge-commit` hook lets a merge commit through during an unverified task when a remote-tracking ref contains every
  commit it takes in, so merging the fetched base branch, or `git pull`, keeps working; a merge of a local branch still stops (git hooks).
- The docs say which usage store each `cw metrics` command reads and which wins when they differ; the context composition moves to
  its own experimental package, so the usage import stands alone, with `cw metrics` output unchanged (usage stores).
- Each `cw task` subcommand is built by its own constructor in its own file, so contributors change one command
  without touching the others; help, flags, output and exit statuses are unchanged (development).
- `cw doctor` lives in `internal/doctor`, so the commit gate no longer imports the credential store, Graphify or go-toml;
  `cw doctor` counts a Graphify runtime as `graphify_cli` only when you may execute it (development).
- Task rules read the source, the clock and task artefacts through ports (`taskflow.Env`, `cli.Config.Now`),
  so a rule test uses fakes instead of a Git repository or waiting (ports).
- Task rules check consultation records through a validator port and the `internal/consult/record` summary, so
  `internal/taskflow` no longer links the adviser HTTP providers or `net/http` (ports).
- `taskflow` types its refusals (locked, abandoned, revision limit, invalid input), so code and tests tell them from failures with
  `errors.Is`; messages, JSON output and exit statuses are unchanged (typed errors).
- `cmd/cw` tests build `cw` once per run and run in parallel (about 90 s to 25 s locally), and the `internal/taskflow` tests of the
  4,096-revision limit run at 64 revisions, with one test pinning the shipped value (development).
- Cheaper default models for simple work: Claude Code lookups run on Haiku 5.5 and summaries on Sonnet 5.5; Codex Sol runs at `high`, keeping `max`
  for planning, architecture and diagnosis, and recommended Codex memory passes use Luna with no context window override (routing).
- Agents states why the reviewer and test engineer do not run in a Claude Code worktree: it would hold
  the wrong source and does not block reads, so their independence rests on the contracts and briefs.
- `cw task verify` runs a list of rules whose findings carry stable codes, and `--summary` selects by code, so rewording a
  message no longer returns a fixed caveat to `verify` or `doctor` summaries; JSON output is unchanged (rules).
- `cw task verify` names an unresolved, stale or invalid decision "coordinator decision" instead of "Codex task decision", since either host
  may coordinate; the problem's place, exit status and JSON keys are unchanged (diagnostics).
- The session-start tests no longer depend on its 2 s and 5 s deadlines, so a loaded CI machine does not fail them; cw keeps
  those deadlines and its session-start output unchanged (session start).
- The task-orchestration skill starts every stage with a fresh worker from `cw task brief` and names one file per step, and each agent
  returns at most the capped 6 KiB result, the rest in its report file (skill).
- Lint fails a new function above the complexity limits (`gocognit`, `funlen`, `gocyclo`) and unchecked `Close`, `Remove` or print results outside
  today's listed offenders, a list that only shrinks (CI).
- `cw` links `golang.org/x/sys` v0.50.0, up from v0.49.0; `THIRD_PARTY.md` lists the new version.

### Fixed

- On macOS, a cgo Go check earns credit only from a developer directory the user cannot change: the one `xcode-select -p` prints, or else
  the Command Line Tools. The run gets that directory as `DEVELOPER_DIR` and xcrun's lookup cache off, so a user-owned `Xcode.app`
  no longer earns it, and a relative `DEVELOPER_DIR` is refused. Under the fallback the check and its tests run with the Command Line
  Tools, not the selected Xcode, and a standard Xcode under `/Applications` with no Command Line Tools installed earns no credit
  (the C toolchain).
- After the wall clock steps back, `cw task` commands keep writing: stored times run ahead of the clock until it catches up, so runs
  made meanwhile show equal starts and `0s` in the timeline (task record format).
- A failed or interrupted `cw consult` no longer leaves an empty file at `--output`, so a retry with the same path works;
  an existing file there is still never replaced (consultation).
- `cw task start` and `cw consult` no longer fail when only some adviser key variables are set and the configuration directory
  is not owner-only: they skip the saved keys, use the environment ones and warn (adviser keys).
- `cw publish --help` says a run appends its `started` entry only once its checks pass, so a run they deny or refuse
  appends none, as the docs do (diagnostics).
- `go test ./internal/skillcheck` passes in a checkout made under umask 002: it checks an imported file's bytes
  and executable bit, not its other permission bits (content checks).
- `go test ./internal/cli` passes wherever the checkout sits: the relative `--home` test resolves against the working directory
  the command is given, as `cw` does, not the test's own (paths).
- Release notes also leave out scoped `docs`, `test` and `chore` commits, such as `chore(release):`; a breaking one
  (`chore!:`) stays (releases).
- Three links in the Go documentation and gopls skills now land on their section, and `go test ./internal/skillcheck`
  checks each Markdown `#anchor` against the target's headings (content checks).
- The docs say what a refused `cw publish` leaves: nothing when its checks refuse, even for one file of the set, and the commit
  or push already done when a later step refuses (diagnostics).
- A failed `cw task bundle` write leaves no partial bundle, and the bundle is still written on a filesystem
  without hard links (diagnostics).
- The hook docs record live runs: Claude Code asks before `cw task aban''don` too, so it is no longer listed as a bypass, and Codex
  runs both handlers and adds the conventions again on each resume (recorded host runs).
- The hook's asks about a record cw cannot read, a `cw task abandon` and a push of uncommitted changes name at most
  three tasks and count the rest, as the deny does (asking).
- `cw publish` refuses a remote Git hands to a remote helper (`foo::…`, `remote.<name>.vcs`, or a scheme other than https, http, ssh,
  git+ssh, ssh+git, git or file in lower case) as 4 `remote-split` (publish).
- Resuming a Codex thread no longer adds the conventions again, nor a second `cw gate install` line beside the compaction that
  can follow; startup and compaction still add them (session start).
- The checkpoint reference again says how a task across several repositories sends one adviser request (`state.task_contexts`),
  a rule the first cut of the skill dropped (advisers).

- The four test-engineer eval prompts name the `test-strategy` references to apply by path; they still pointed at
  links the test-engineer contract dropped when it was shortened. `go test ./evals` checks that each named file exists.
- The fork-swarm cancellation test of `internal/graphify` counts a trial only when the kill finds all 32 forkers forking and
  starts it again otherwise, at most ten times a run; a loaded macOS runner failed it twice when one forker never reported.
- The agent evals no longer fail an agent for the harness: a run can call `cw task schema handoff`, the `go test` graders accept
  `go -C <dir> test`, two graders ask only what the contract and brief ask, and the `loader-once-race` fixture has one defect,
  not two (evidence).
- The eval workflow copies workspaces out of the sealed run directories, allows 3 US dollars per case where four cases got
  6.67 and `architect` cost 8.39, and fails a dispatched run that has no API key (what CI runs).

## [3.0.0-rc.2] - 2026-11-12

The first published v3 release candidate (`v3.0.0-rc.1` was never published): every change since v2.0.96.

### Upgrade steps

1. From v2.x there is no upgrade path: install v3 fresh, keep `workflow-tasks/`, `credentials.json` and `preferences.json`,
   and remove other v2 files by hand (install, paths).
2. Finish open v2 tasks first, or move their directories out of the task store: v3 refuses records from cw v2.0.66 to v2.0.96,
   and an open one gates commits and pushes until moved (which records gate).
3. This release writes task record version 4: run the same `cw` on every host and hook that shares a task store
   (format).
4. Exit statuses follow one table, and several changed: update scripts that test them (exit statuses).
5. Under Codex, trust the plugin hooks in `/hooks` after installing or updating the plugin: trust follows the hooks' hash,
   and `hooks/hooks.json` changed (Codex).
6. Optionally, run `cw gate install --repo <path>` in each repository, so git runs the gate for commits and pushes
   from scripts, aliases and terminals too (installing).

### Breaking

- The v2 installer is removed: install the Claude Code or Codex plugin instead; `cw install --host claude`
  now points at the plugin (install).
- v2.x is end-of-life: no fixes, no upgrade or cleanup path. Install v3 fresh, keep `workflow-tasks/`,
  `credentials.json` and `preferences.json`, and remove other v2 files by hand (paths).
- `cw` refuses task records from cw v2.0.66 to v2.0.96 (no `packs` or `test_rules`), and an open one gates until moved:
  finish open v2 tasks before upgrading, or move them out of the task store (which records gate).
- The module path gains `/v3`: install with `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.2`
  and import `github.com/belevtsev/agent-workflows/v3/...` (get cw).
- Worker agents lose the `cw-` prefix and policy role `testing` is split by stage: dispatch
  `agent-workflows:reviewer`, `:test-engineer` and `:github-worker` (agent types).
- Task records move to version 4 and gain members that an older `cw` cannot read, and its hook skips such a task instead of gating it:
  run the same `cw` on every host and hook that shares a task store. A `cw task` command on a record from a newer `cw`, or without
  `packs`, `test_rules` or `baseline_manifest`, exits 4 (format).
- `cw task start` refuses a substantial plan without a check bound to a requirement: add one, and
  restart open tasks that have none (start a task).
- New tasks in Go modules get the `go.package-test/v2` rule in `cw task verify`: add direct `go test -race`
  checks covering each changed package, or opt out with `cw pack remove go` (Go rule).
- `cw task verify` needs a proven PASS for each mapped test and an independent test-engineer author
  for each changed Go test file: have the test engineer write them (test authorship).
- Nothing writes the hosts' model and effort settings any more: set them yourself in `settings.json`
  or `config.toml` (local configuration).
- `cw task start` refuses a not-required checkpoint reason that claims no advisers while the task binds
  some: reword the reason or keep the checkpoints required (checkpoints).
- `cw task assign` needs `requirements` on a brief with a `contract` exactly when the plan has a
  per-test-credited `go test -json` check: declare them then, and only then (briefs).
- A key variable such as `OPENAI_API_KEY` alone no longer binds an adviser: opt in with `advisers.enabled`, `cw keys set`
  or `--policy`; tasks already started keep their binding, and `cw doctor` names a key without an opt-in (configure advisers).
- Exit statuses follow one table: an incomplete `cw task verify`, a failed `cw task run` check and a partial `cw consult` exit 5,
  Graphify unavailable exits 4, `cw publish` unknown and mismatch exit 6 and 7: update scripts that test them (exit statuses).

### Added

- Claude Code and Codex plugins in one marketplace, with the Go skills as the `cc-skills-golang`
  plugin (install).
- `cw install|uninstall --host codex` links or removes a checkout's skills and `AGENTS.md` block
  (Codex).
- Release archives for macOS and Linux (amd64, arm64) with `SHA256SUMS` (get cw).
- Seven worker agents, each with a role contract: spec-writer, architect, implementer, test-engineer,
  reviewer, github-worker and the opt-in adversarial-tester (agents).
- A Claude Code hook denies `git commit` and `git push` while an open substantial task has no passing
  `cw task verify`, and asks before large unrecorded changes (hooks).
- The commit hook follows `cd` and `git -C`, including `cd "$PWD"`, `cd ~/proj` and `git -C "$HOME/proj"`; after `cd $OTHER`, `cd -`,
  `popd` or another change it cannot follow, it checks the session's directory and asks instead of denying (recognised commands).
- A task record `cw` cannot read makes the commit hook ask and `cw publish` exit 3: remove or move its directory to clear it.
  So does a store the hook cannot locate or read (records).
- Under Codex, which runs the plugin hooks once you trust them in `/hooks`, the commit hook denies where Claude Code asks, because Codex cannot ask,
  except about a record or store it cannot read, which goes ahead; the docs say how to set options there (Codex).
- The gate's threat model and known bypasses:
  it guards a cooperative agent, not against a hostile one.
- `cw publish` commits, pushes and opens or edits the pull request of a verified task
  (publish).
- `cw task add-checks` adds covering `go test` checks after a task merges its base branch
  (merging the base).
- `cw task abandon` closes a task so it stops gating; the commit hook asks before it, and under Codex denies it,
  and gate messages say to ask you before abandoning (abandoning a task).
- Requirement traceability: handoffs map a test engineer's `requirements` to tests, and `verify`
  judges each mapped test (handoffs).
- `cw task trace`, `cw task authorship` and `cw task timeline` report requirement coverage, test
  authorship and wall time, read-only (diagnostics).
- Review findings carry a `severity`, and `verify` lists rejected `blocker` and `major` findings
  (acceptance).
- Language packs: the built-in Go pack, on by default, and `cw pack list|add|remove` (packs). The packs page
  documents `cw pack` and `pack.yaml`, with the steps and a proposal template for adding a language pack.
- `cw keys set|unset|status` manages adviser keys, never from the command line; disable an adviser in
  `preferences.json` (adviser keys).
- `cw metrics import --append` keeps a cost history that outlives transcripts, and `cw metrics history`
  reports it (cost history).
- The `codebase-design` building blocks
  reference, which the architect, implementer and test-engineer contracts apply.
- Eval suites for six of the agents, checked offline by `go test ./evals/...` and run weekly in CI
  (evals).
- `LICENSE` (MIT), `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `SECURITY.md`, issue and pull request
  templates, CODEOWNERS, Dependabot and folder guides (contributing).
- `cw hook git pre-commit` and `cw hook git pre-push` run the commit gate from git's own hooks, which also see aliases and scripts:
  a gating task exits 3, and each ask is a warning; `CW_GATE=off` or `--no-verify` skips it (git hooks).
- A check can name variables such as `DB_PASSWORD` in `env_passthrough` to receive them, where `cw` otherwise drops names with `KEY`, `TOKEN`,
  `SECRET`, `PASSWORD` or `CREDENTIAL`; `cw task verify` lists each as a limitation (check environment).
- `cw gate install`, `uninstall` and `status` write, remove and report a repository's git pre-commit and pre-push hooks that run that gate;
  they never change a hook cw did not write, or a hooks directory other repositories use without `--shared` (installing).
- The session-start context gains one line when the plugin and `cw` versions differ, naming the one to update, and one
  when Claude Code is older than 2.1.292; a `cw` built between tags is not compared (session start).
- While a substantial task is open in a repository without git's hooks, the session-start context suggests `cw gate install` once
  for that task and `cw doctor` reports `git_hooks` as missing; it never installs them (reminders).

### Changed

- Substantial tasks review each stage (spec, design, tests, code), send findings back to its author, cap
  revision rounds (review stages) and fix a wrong contract by amendment.
- Code review checks the source against the contract, `design.md` and the coverage profile, with a
  failing test for each behaviour finding (code review).
- The reviewer and test engineer start without CLAUDE.md files (Claude Code 2.1.271 or newer); briefs
  quote the rules they need (agent types).
- In a repository with `docs/decisions/`, the architect also writes an ADR for a choice that is costly
  to reverse (architect).
- The always-on `global/AGENTS.md` block and the `task-orchestration` skill core keep only the decision
  flow; the rules live in references (skill).
- `cw` no longer reads its model policy or configuration from an installed release
  (policy, paths).
- Diagrams are archify specs with committed SVGs; Mermaid is gone (diagrams).
- `cw` runs on macOS and Linux; Windows is not supported (get cw).
- CI runs gofmt, vet and tests on Linux and macOS, plus golangci-lint and govulncheck for both
  (CI); releases come only from pushed `v*` tags and
  build with go1.27.4, which fixes 11 reachable stdlib vulnerabilities (releases).
- Both plugins and the marketplace carry version `3.0.0-rc.2`; install `cw` from the `v3.0.0-rc.2` release instead of `@main` (get cw).
- The README's first task is a substantial one, with a line for small changes and the planned, unmeasured per-task
  cost caps (first task).
- Without `cw` on `PATH`, the session-start hook prints its install command, and `global/AGENTS.md` and the
  task-orchestration skill say to stop and give it (session start).
- The overview diagram ends with `cw publish`, with the GitHub worker as fallback, and shows the opt-in adversarial
  tester (how it works).
- Security fixes go to the newest v3 release, release candidates included (supported versions).
- The commit gate's deny names at most three open tasks, each directory once, and counts the rest with their task store,
  so it stays short with many open tasks; the Codex deny and git hook stderr follow (which records gate).
- The commit gate's rules live in `internal/gate`, so contributors unit-test them with a fake git, clock and task store
  (development).
- The credential store, cost history and Graphify cache share one `flock` lock in `internal/fslock`,
  released when its holder dies; new locks reuse it (development).
- One table compares the Codex install shapes and recommends the plugin with its hooks trusted; untrusted, it gives
  skills only, and `cw install --host codex` adds archify but no commit hook (Codex).
- Task checks run on the shared `internal/proc` runner through `taskflow.RunCheck` (was `taskflow.Run`), so contributors change
  a check's program, environment or bounds in one function, `checkSpec`; check records are unchanged (development).

### Fixed

- Source capture reports a Git probe that does not finish cleanly instead of treating the repository
  as a plain folder (diagnostics).
- `cw metrics import --composition` no longer slows down on long or deeply nested Bash commands
  (diagnostics).
- `cw consult` reports a failed close of its output record instead of exiting 0 with a record that
  may be incomplete.
- `THIRD_PARTY.md` lists go-toml v2.4.3, the version `cw` links; a test keeps the table in step with `go.mod`.
- Worker agents can no longer dispatch subagents or run workflows, and the README states the minimum Claude Code version:
  2.1.292, for per-dispatch `effort`; below 2.1.271 `omitClaudeMd` is also ignored silently (agents).
- A relative `XDG_STATE_HOME` is refused with exit status 1, and the commit hook asks, instead of joining it to the working
  directory, which moved the task store, and the gate with it, on every `cd`; set an absolute path (paths).
- `cw doctor --live` probes and Git inspection kill their process group until it is empty, as checks already did, so a child forked
  as the kill lands cannot outlive them (one kill can miss it on macOS) (diagnostics).
- The `go list` runs behind `cw task verify`'s Go facts kill their process group when they end, so a child of `go` or a `go` wrapper
  no longer outlives them or their time budget; `cw publish` and `cw graphify` share that runner (development).
- A killed `cw` no longer leaves its task, or the task's `cw publish`, locked, and a second task writer waits up to 10 s instead of failing;
  do not mix `cw` versions on one task (task locks).
- A check named `go` runs only a `go` built from `cmd/go`, with its own GOROOT, through shims and toolchain switches, and records it, and
  `cw task verify` reads its Go facts from such a `go`: a fake `go` on `PATH` fails the check (`untrusted_toolchain`) instead of passing
  and cannot waive `-race`; put the real `go` first on `PATH` (which go).

### Removed

- The v2 installer: `install.sh`, the setup wizard, `cw status|update|rollback|recover` and the
  release snapshots (install).
- `cwdev check`, `cwdev release` and `cwdev sandbox` (development).
- The `db-postgres` third-party skill, whose license was ambiguous
  (license rule).
- Archify's rendered HTML examples and the Go pack's `EVALUATIONS.md`, 13 MB of the plugin download;
  `archify examples --output-dir <dir>` renders the examples (third-party notices).
- The Go rule `go.package-test/v1` is gone: `cw` refuses a task that binds it, which only development builds before
  v3.0.0-rc.1 started, and an open one gates until moved; start a new task (format).


## [2.0.96] - 2026-11-07

Historical final automatic `cw` release from main. Its tag and release assets were removed on
17 November 2026; the version link retains its source commit.

[Unreleased]: https://github.com/belevtsev/kitsu/compare/v0.0.2...main
[0.0.2]: https://github.com/belevtsev/kitsu/releases/tag/v0.0.2
[0.0.1]: https://github.com/belevtsev/kitsu/releases/tag/v0.0.1
[3.0.0-beta.1]: https://github.com/belevtsev/kitsu/commit/511ceb11c1946c4185c5a93c0c0a9e79613bdc3d
[2.0.96]: https://github.com/belevtsev/kitsu/commit/1f26165884e4f6047e6ca12da15fba7bc378b37e
