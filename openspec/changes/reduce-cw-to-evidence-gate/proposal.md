# Proposal: reduce-cw-to-evidence-gate

Status: on 14 November 2026 the owner accepted the eight recommended answers
under Open Questions. Nothing here is implemented. What stays, moves and is
cut is in [keep-and-cut.md](keep-and-cut.md); the order of the work is in
[tasks.md](tasks.md).

Later the same day the owner changed one answer and added one requirement:
Windows is in the first version, and installing `cw` must not need Go. Both
are in the capability `platform-support`. The owner also decided that a
release archive is enough for the first version: no package manager. Open
Questions 9 to 11 are new and open.

Still the same day the owner said that the tool must have roles, with a model
for each, and a count of the tokens the workflow costs. That is a second
change, [add-role-workflow](../add-role-workflow/proposal.md), built on this
one. If the owner accepts it, the answer to Open Question 1 changes: the roles
are not an optional layer elsewhere but the second part of this tool, in this
repository. No requirement and no task of this change depends on that.

## Why

The strict review of commit `ce9b122` (see Sources) found one idea in this
project that other people could use, and a product around it that they cannot
adopt.

The idea is sound and rare. A coding agent's "done" is a claim. `cw` replaces
the claim with a record: it runs the checks itself, binds the result to the
source, and blocks `git commit` and `git push` until the checks pass. The
popular projects in this space are prompt-only and enforce nothing at commit
(review report, "Comparable projects").

What stops other people from adopting it today:

- **Too much to learn.** About 45,000 lines of production Go, and a task record
  with a plan, profiles, stages, rounds, briefs, bundles, handoffs, acceptances
  and assessments. In the review's reproduction, the default (lite) flow needed
  four hand-written JSON documents (brief, result, acceptance, assessment) and
  one edited one (the reviewer bundle) before `cw task verify` passed.
- **Go only.** The evidence rule understands `go test` and nothing else
  (`internal/langpack/golang.go:13-51`). Six findings come from that one rule
  and its C-toolchain layer (H3, H4, M6, M7, L6, L14).
- **The gate has holes where it matters.** It allows an unverified commit when
  it cannot fingerprint the source (C2, `internal/taskflow/find.go:254-256`).
  It misses a new file left out of `git add` (H1). The plugin hook decides by
  parsing shell commands, and three ordinary command shapes get past it (M1,
  M2, M3); four of eight mutants in that code survive its tests (H8).
- **The task lifecycle blocks normal work.** A finished task keeps denying the
  next edit, and a task in the main checkout blocks every linked worktree (H9).
  A killed check run stays "running" for ever (H2).
- **It cannot be installed as documented, and its benefit is not measured**
  (C1, H7).
- **It can send repository details to third parties** (H5).

This change keeps the idea and removes the rest. The whole promise of the core
is one sentence:

> A commit is allowed only when every check the repository declares has a
> recorded passing run, made by `cw`, on exactly the content of that commit.
> A push is allowed only when the same holds for the commit it puts at the tip
> of a remote ref.

The intended user is any person or team that lets a coding agent commit, in any
language, with any agent, on Linux, macOS or Windows.

## Terms

- **Declaration**: the file `cw.toml` at the repository root. It lists the
  checks.
- **Check**: a program and its arguments, with an `id`. Optionally `paths`, the
  files the check depends on, and a `timeout`.
- **Snapshot**: the git tree that `git add -A` followed by `git commit` would
  record from the working tree. It holds every tracked file as it is on disk and
  every untracked file that git does not ignore.
- **Run**: one execution of one check by `cw run`, recorded with the snapshot it
  ran on.
- **A path differs** between two trees when it exists in only one of them, or
  its content or its mode is different. Renames and copies are not detected, so
  a moved file differs at its old path and at its new one.
- **Same checked content**: for one check, two trees hold the same checked
  content when no path that differs between them matches the check's `paths`.
  For a check without `paths`, only identical trees do.
- **Satisfied**: a check is satisfied for a tree when its most recent decisive
  run on the same checked content passed. A decisive run is one that ended
  `passed`, `failed` or `timed_out`. The most recent is the one that ended
  last.
- **Verified tree**: a tree with a valid declaration in which every declared
  check is satisfied.

## What Changes

- **BREAKING** `cw` becomes a gate with ten commands: `cw init`, `cw add`,
  `cw run`, `cw status`, `cw stats`, `cw context`, `cw hook`, `cw guard`,
  `cw uninstall` and `cw version`. (`cw guard` was added during the build: it
  is the plugin's question before a tool call, kept in `cw` so that the hook
  files hold no logic.) The commands `cw task` (23 subcommands), `cw publish`,
  `cw consult`, `cw keys`, `cw metrics`, `cw pack`, `cw doctor`, `cw gate` and
  `cw install` leave the core.
- **BREAKING** Checks are declared in a committed file, `cw.toml`, not in a
  per-task JSON plan. A check is any program: a test command, a linter, a
  coverage floor, a mutation test, a script that calls a review agent. `cw`
  knows nothing about any language.
- **BREAKING** Evidence is bound to a git tree, not to a task. There is no
  task, plan, profile, stage, round, brief, bundle, handoff, acceptance or
  assessment in the core, so there is nothing to start, close or abandon.
- **BREAKING** The gate is git hooks only: `pre-commit`, `pre-merge-commit` and
  `pre-push`. They judge the tree the commit records and the refs the push
  sends. Nothing parses shell commands.
- **BREAKING** The gate fails closed. When it cannot decide, or `cw` is missing,
  it denies. Today it allows.
- **BREAKING** Records live inside the repository's git directory, not under
  `~/.local/state`. One small record per run; no revision chain.
- `cw` opens no network connection and sends nothing anywhere.
- The Claude Code and Codex plugin shrinks to two jobs: tell the agent the
  repository is gated, and ask the user before the agent skips the gate.
- New: `cw stats` counts, from local records, what the gate allowed, what it
  denied and why. New: an observe mode records what the gate would have denied
  without denying, so each adopter can measure the benefit before enforcing.
- New: `cw add` writes a check into `cw.toml` from the command line, so the
  first gated commit needs no hand-written file.
- New: `cw` runs on Windows as well as on Linux and macOS. Today Windows is not
  supported (`docs/operations.md:131`).
- New: each release has an archive for each of the three systems, and the
  first install instruction needs no Go toolchain. Today the README lists Go as
  a requirement and names a release that does not exist (C1).

No migration is owed. No `v3` tag was ever published (C1), and `v2` is end of
life.

## Capabilities

### New Capabilities

- `check-declaration`: the `cw.toml` format, its validation, `cw init` and
  `cw add`.
- `check-evidence`: the snapshot, running checks, what a run records, when a
  check is satisfied, when a tree is verified, and the record store.
- `publish-gate`: the git hooks, the commit rule, the merge rule, the push rule,
  failing closed, denial messages, observe mode and `cw uninstall`.
- `gate-reporting`: `cw status`, `cw stats`, recorded decisions and exit
  statuses.
- `agent-integration`: what the Claude Code and Codex plugin does and does not
  do.
- `platform-support`: Linux, macOS and Windows; programs and hooks on Windows;
  a check whose program a system lacks; installing without a toolchain.

### Modified Capabilities

None. `openspec/specs/` holds no capability yet, so the behaviour this change
removes has no specification to modify. The removals are listed under What
Changes and Non-goals.

## Impact

- **Code.** The core needs a declaration reader, a check runner, a record store,
  three hook decisions and the ten commands above. Of today's packages, the ones
  that carry removed features are `internal/taskflow` (12,907 lines),
  `internal/usagelog` (4,539), `internal/consult` (3,990), `internal/langpack`
  (2,625), `internal/publish` (2,623), `internal/graphify` (1,284),
  `internal/shellwords` (1,048) and most of `internal/gate` (2,702) and
  `internal/cli` (5,950). Which code is kept, moved to an optional layer or
  retired is in [keep-and-cut.md](keep-and-cut.md).
- **Users.** Today's users are the owner. The `cw task` flow stops working in
  the core.
- **Skills and agents.** The seven roles and the orchestration skill are not
  part of the core. See Open Question 1.
- **Documentation.** README, quick start, SECURITY.md and docs/hooks.md
  describe the removed flow and need rewriting for the core.
- **Windows.** Process control and file locks exist for Linux and macOS only.
  On other systems `internal/proc` stops only the program itself and
  `internal/fslock` can take no lock (`group_other.go`, `fslock_other.go`).
  Both need a Windows implementation, and CI needs a Windows job.
- **Release.** The core needs a tagged, installable release (C1) before anyone
  else can try it, with archives for the three systems. Today's release
  configuration builds for darwin and linux only (`.goreleaser.yaml`).

## Non-goals

- Tasks, plans, profiles, stages, rounds, worker briefs, bundles, handoffs,
  acceptances, assessments and coordinator decisions.
- Roles and multi-agent orchestration. A team that wants an independent review
  can declare a check that runs one.
- Adviser consultation, adviser keys, usage budgets and cost tracking.
- Language rules: the Go evidence rule, per-test credit, the flag allowlist and
  the C-toolchain trust layer. A check passes when its program exits 0.
- `cw publish`, pull-request creation, Graphify, packs, the doctor and the
  Codex installer.
- Defence against a hostile agent or person. Someone who forges a record, edits
  a hook or uses `git commit --no-verify` gets past the gate. The gate is a
  guardrail for a cooperative agent; server-side enforcement stays with CI and
  branch protection.
- Proof about the environment. A passing run says the checks passed on this
  content on this machine. It does not capture the toolchain version, ignored
  files, environment variables or network state.
- Sharing evidence between clones or with CI. CI runs the same declaration
  again (`cw run --base`).
- Running checks in parallel, running checks inside a hook, and checking a
  partial commit (`git add -p`) in a separate copy of the tree.
- Package managers such as Homebrew, Scoop, winget or npm in the first
  version. `cw` is installed from a release archive. The owner decided this on
  14 November 2026.
- Version control systems other than git.
- Sparse checkouts in the first version. `cw` refuses to run where tracked
  files are kept off the disk on purpose (`check-evidence`: Working tree that
  cannot be recorded).

## Known limits

These follow from the design and are stated so that nobody is surprised.

- Git does not version hooks. Each clone runs `cw init` once. A clone without
  hooks is not gated; CI is the backstop.
- `paths` is a promise by the repository that the check reads nothing else. A
  wrong `paths` carries a pass over to content the check never saw.
- A partial commit is denied, because the checks ran on the whole working tree.
  The way through is to stage everything, or to stash the rest and run again.
- Git runs `pre-commit` for `git commit`, and when a conflicted
  `git cherry-pick`, `git merge` or `git revert` is continued. It does not run
  it for a clean `git cherry-pick` or `git revert`, for `git rebase` (also when
  continued) or for a fast-forward merge (observed, see Sources). The push rule
  judges what those leave at the tip.
- The push rule judges the tip commit of each ref, not every commit below it.
  After a rebase or a cherry-pick of several commits, the commits below the tip
  were never checked one by one. CI on a pull request has the same limit. See
  Open Question 8.
- A run is bound to the tree git records, after git's own conversions: line
  endings, clean filters and `core.fileMode`. It is not bound to the bytes on
  disk.
- When another tool's `pre-commit` hook changes files after `cw hook
  pre-commit` has run, git records a tree that `cw` did not judge (observed).
  `cw hook` must therefore be the last command of a shared hook. If it is not,
  the push rule still denies the resulting commit.
- When `core.hooksPath` is set, the hooks belong to the owner or to a hook
  manager, and `cw init` writes none. A linked worktree is then gated only when
  those hook files exist in it (observed: a relative path with untracked hook
  files leaves a worktree without hooks).
- Git runs `pre-push` for `git push --dry-run` as well and gives the hook no
  sign of it (observed). A dry run is therefore judged like a push.
- A submodule is recorded by its checked-out commit. Uncommitted changes inside
  a submodule are not part of the snapshot.
- On Windows `cw` needs Git for Windows; its shell runs the hooks. A Linux
  distribution under the Windows Subsystem for Linux counts as Linux.
- One `cw.toml` holds the same checks on every system. A check that names a
  program only some systems have, such as `sh` or `make`, fails on the others.
  `cw` does not translate commands (`platform-support`: Programs that a system
  lacks).
- Nothing in this specification was observed on Windows. The git behaviour it
  relies on was observed on Linux only, so each requirement may need a
  correction once the tests run on Windows (tasks 3.4 and 3.5).
- On Windows git does not record a change of the executable mark, and file
  names that differ only in case are one file on disk. Both are git's own
  behaviour, and the snapshot follows it.
- CI tests one architecture per system. The archives for the other
  architecture are built and not tested, as today.
- The plugin sees `--no-verify` only as literal text. `git commit -n` is not
  caught by the plugin; the push rule still catches the resulting commit.

## Success measures

The change is done when these hold, each checked by a test or a published
number:

1. **First gated commit.** On a machine with `cw` installed, a new repository
   reaches its first gated commit with five commands (`cw init`,
   `cw add <id> -- <command>`, `cw run`, `git add -A`, `git commit`) and no
   hand-written file. A test executes exactly this on each of the three
   systems, as the current quick start test does on two.
2. **Hook cost.** A hook decision takes at most 150 ms on this repository
   (2,260 files) and at most 500 ms on a repository of 30,000 files and
   68 MiB, measured in CI on Linux and macOS. The git plumbing alone took
   24 ms for the snapshot of the first, and 73 ms for the snapshot, the
   commit's tree and the differing paths of the second (see Sources). The
   limits for Windows are set after the cost is measured there (Open
   Question 10).
3. **Size.** The core is at most 6,000 lines of production Go. Today's `cw` is
   about 45,000.
4. **Install.** The first install instruction in the README needs no Go
   toolchain: it downloads the archive of a tagged release for Linux, macOS or
   Windows and checks it against the published checksums. The release is built
   from the commit the README describes, and CI executes the instruction on
   the three systems (`platform-support`: Install without a toolchain).
   `go install` is the second instruction.
5. **Measured benefit.** Before the README claims a benefit: at least three
   repositories in at least two languages run observe mode for two weeks of
   agent work, and the README reports, from `cw stats`, the share of commits
   that would have been denied and why. This is the baseline the review asked
   for in H7.

## Review findings this change answers

"Removed" means the feature that holds the defect leaves the core. It does not
mean the defect is fixed in code that stays elsewhere.

| Findings | Answer |
| --- | --- |
| C1 | Success measure 4. The plugin holds no `cw` version (`agent-integration`: Instruction text comes from `cw`). |
| C2, M5 | `publish-gate`: Fail closed. `check-evidence`: Snapshot (no size ceiling). |
| H1 | `check-evidence`: Snapshot holds untracked files. `publish-gate`: Commit rule, Paths that differ. |
| H2 | `check-evidence`: Interrupted runs. |
| H9, L2 | No task exists. `publish-gate`: Commit rule (scenario "The next change needs only a new run"), Linked worktrees. |
| M26 | `publish-gate`: Push rule judges the refs git passes. Deletes pass. A dry run is judged like a push (Known limits). |
| H8, M1, M2, M3 | `agent-integration`: The plugin never judges a commit or push. No shell-command parsing remains. |
| L1 | `publish-gate`: Denial message. |
| H7 | `gate-reporting`: Statistics. `publish-gate`: Observe mode. Success measure 5. |
| H5 | `check-evidence`: No network. Consultation is removed. |
| M4, L8 | `check-evidence`: Record store (one record per run; a record that cannot be read is ignored). |
| M8 | `check-evidence`: Satisfied check (the most recent decisive run decides). |
| M9 | `check-evidence`: Running checks (environment unchanged). |
| M13 | `check-evidence`: Outcome of a run (`source_changed` names the paths). `cw publish` is removed. |
| M14 | `check-evidence`: Log limit. |
| L7 | `check-evidence`: Leftover processes. |
| L10 | `check-declaration`: Invalid declaration (unknown keys are refused). |
| H3, H4, M6, M7, L6, L14 | Removed: the Go evidence rule and the C-toolchain layer. |
| M10, M11, M12, L5, L9 | Removed: consultation, budgets, `cw publish`, credentials. |
| H6 | Not claimed: the core makes no statement about independent tests or review. |
| M15, M16, M17, M18, M25, L11, L15 | Not answered here: they concern the skills and the workflow layer (Open Question 1). |
| L3, L4 | Removed: the Codex installer. |
| M19, M20, M21, M22, M23, M24, L12, L13 | Not answered here: release process, repository settings, code and documentation quality. They belong in the task list. |

## Independent review of this draft

One reviewer challenged the first draft on 14 November 2026 and returned 16
findings. All 16 are resolved in this text; none was rejected and none is
deferred. "Ran" means the claim was re-run with git (second script under
Sources); "read" means it was worked through against the text.

| # | Finding | Checked | Resolved by |
| --- | --- | --- | --- |
| 1 | A push judges only the tip, so commits below it are never checked; the draft claimed otherwise. | read | The promise, `publish-gate` Push rule and scenario "Only the tip is judged", Known limits, Open Question 8. |
| 2 | With skip-worktree, assume-unchanged or a sparse checkout, the checks run on files that the tree does not hold. | ran | `check-evidence`: Working tree that cannot be recorded. Sparse checkouts are a non-goal. |
| 3 | Another tool's `pre-commit` can change the tree after `cw` judged it. | ran | `publish-gate`: Hooks of other tools (the `cw hook` line comes last). Known limits. |
| 4 | A relative `core.hooksPath` leaves linked worktrees without hooks, and hook files in the working tree enter the snapshot. | ran | `publish-gate`: Hook installation and Hooks of other tools (`cw` writes hooks only in the git directory, and none when `core.hooksPath` is set). Known limits. |
| 5 | "Updates that add nothing" let an unchecked remote branch reach `main` by a fast-forward. | read | `publish-gate`: Updates that add nothing (a branch counts only against itself) and a Push rule scenario. |
| 6 | Rename detection would decide whether a pass carries over. | ran | `check-evidence`: Same checked content. Terms. |
| 7 | Success measure 1 could not pass: `cw.toml` is untracked after `cw init`. | ran | Success measure 1 (five commands). |
| 8 | Line endings, clean filters and `core.fileMode` separate the bytes on disk from the tree. | read | Known limits. The behaviour is unchanged; the limit is now stated. |
| 9 | A denied push of a branch that is not checked out had no single command to run. | read | `publish-gate`: Denial message and scenario "The branch that is pushed is the one judged". |
| 10 | "Most recent" run had no ordering. | read | `check-evidence`: Satisfied check and scenario "Two runs that overlap". |
| 11 | Whether `dir/**` matches a submodule or a symbolic link at `dir` was open. | read | `check-declaration`: Path patterns. |
| 12 | The exit status for an invalid declaration inside a hook was contradictory, and observe mode was undefined when no decision is possible. | read | `check-declaration`: Invalid declaration. `publish-gate`: Fail closed, Observe mode. |
| 13 | The limit about which commands run `pre-commit` was wrong for continued operations. | ran | Known limits. `publish-gate`: scenario "A continued cherry-pick is judged". |
| 14 | After `cw uninstall`, another tool's hook that calls `cw hook` denies every commit. | read | `publish-gate`: Removal. `gate-reporting`: Exit statuses. |
| 15 | Three scenarios could not be built from the shared fixture. | read | Each of the three now has its own setup. |
| 16 | An untracked nested repository without a commit makes `git add -A` fail. | ran | `check-evidence`: Working tree that cannot be recorded. |

## Sources

- `reports/Strict repository review 2026-11-14.md` and
  `reports/Review findings 2026-11-14.md`: the review of `ce9b122`, with
  findings C1 to L15. Both files are local and git-ignored.
- `reports/evidence-gate-git-facts.sh`: a script that tests the git behaviour
  this specification relies on. Run locally on 14 November 2026 with git
  2.53.0 on Linux. It was not run on macOS. Observed:
  - The tree written from a temporary index after `git add -A` equals the tree
    of a commit made after `git add -A`, and the real index is unchanged.
  - Inside `pre-commit`, `git write-tree` gives the tree the commit records,
    for a plain commit, `git commit -a` and `git commit <path>`.
  - `pre-commit` ran for `git commit`, `--amend` and `--allow-empty`. It did
    not run for a clean `git cherry-pick`, a clean `git revert`, `git rebase`,
    a fast-forward merge or `--no-verify`.
  - A `pre-merge-commit` hook that exits non-zero leaves the merge in progress
    ("Not committing merge; use 'git commit' to complete the merge"), and
    `git commit` then concludes it and runs `pre-commit`.
  - `pre-push` receives one line per ref with both commit IDs; a delete has
    the local ID all zeros; a new ref has the remote ID all zeros. It runs for
    `--dry-run` too.
  - Hooks in the common git directory run in a linked worktree.
  - A hook that calls a program missing from `PATH` makes the commit fail.
  - The snapshot took 24 ms on this repository (2,260 files) and 53 ms on a
    synthetic repository of 30,002 files and 68 MiB, which is above both
    ceilings in M5; reading the commit's tree and the differing paths took a
    further 20 ms. The timing commands were run by hand and are not in the
    script.
- `reports/evidence-gate-git-facts-2.sh`: a second script, for the git
  behaviour an independent review of this draft questioned. Run locally on the
  same day with the same git. Observed:
  - When a `pre-commit` hook stages a change after reading the tree, git
    records the changed tree, not the one the hook read.
  - With a relative `core.hooksPath` and untracked hook files, a commit in a
    linked worktree runs no hook.
  - `pre-commit` ran when a conflicted `git cherry-pick`, `git merge` and
    `git revert` were continued, and did not run for `git rebase --continue`.
    A `pre-commit` that denies during `git cherry-pick --continue` leaves the
    cherry-pick in progress, and a later `--continue` concludes it.
  - With a file marked skip-worktree or assume-unchanged and changed on disk,
    and with a sparse checkout, the tree after `git add -A` equals the tree of
    `HEAD`. `git ls-files -v` shows the marks.
  - `git add -A` fails with status 128 when the working tree holds an
    untracked nested repository that has no commit.
  - `git diff-tree -r --name-only` lists a moved file at both paths;
    `git diff --name-only` lists only the new one.
  - In a new repository with an untracked `cw.toml`, `git commit` records
    nothing.
- The owner's decisions of 14 November 2026, given after the first eight
  answers: Windows is needed, installing must not need Go, and a release
  archive is enough for the first version.
- Go's own documentation, read locally with `go doc` from Go 1.27.4:
  - `os/signal`, section Windows: a program can catch Ctrl+C and Ctrl+Break,
    and is told when its console closes or the user logs off, after which the
    system still ends it.
  - `os/exec.LookPath`: on Windows a program name needs an extension named by
    `PATHEXT`.
  - `os/exec.Command`: `cmd.exe`, "and thus, all batch files", unquote a
    command line differently from other programs, so the caller must quote
    for them itself.
- A local cross-build on 14 November 2026: `internal/proc` and
  `internal/fslock` compile for windows on amd64 and arm64. Nothing was run on
  Windows.
- Current behaviour cited above: `internal/taskflow/find.go:254-256`,
  `internal/langpack/golang.go:13-51`, `internal/gate/gate.go:368`,
  `internal/projectctx/capture.go:24-26`, at `ce9b122`.
- `cw --help` and `cw task --help` at `ce9b122` for the command list.

## Open Questions

Questions 1 to 8 are decided. On 14 November 2026 the owner accepted the
recommendation in each of them, and later the same day changed the answer to
question 7. The specifications are written to those answers. The questions
stay in this section because the specifications cite them by number. Where a
question names evidence that would settle it, such as counts from `cw stats`,
that evidence can reopen it later.

Questions 9 to 11 are open. Each has a recommendation, and the specifications
are written to it. On 14 November 2026 the implementer took the three
recommendations, because the owner asked for the work to go on alone
([design](design.md), Decisions taken without the owner).

1. **Where does the multi-agent workflow go?** Recommendation: keep this
   repository and the name `cw` for the core, and move the roles, the
   orchestration skill and the task record to an optional layer that uses the
   core through `cw.toml` checks. Settled by: the owner. The change
   `add-role-workflow` proposes a new answer (its Open Question 2).
2. **Is a partial commit denied?** Recommendation: yes in the first version
   (`publish-gate`: Commit rule). Settled by: the count of such denials in
   `cw stats` from the first adopters; if it is high, add a way to check the
   staged tree alone.
3. **Is a tree without `cw.toml` denied once hooks are installed?**
   Recommendation: yes (`publish-gate`: Fail closed). A gate that allows when
   its declaration is missing repeats C2. The cost is that an old branch
   without the file needs one. Settled by: the owner.
4. **Is a notice enough when a commit weakens the declaration?**
   Recommendation: yes, a notice from the hook plus a question from the plugin
   (`publish-gate`: Declaration change notice; `agent-integration`: Ask before
   skipping). The stricter option is to also require the checks of the
   previous declaration, which makes removing a check impossible without
   `--no-verify`. Settled by: the owner's threat model.
5. **How long does a run count?** Recommendation: 30 days (`check-evidence`:
   Old runs). Settled by: the owner; store size measured after a month of use.
6. **Can Codex ask the user from a hook?** The current gate denies under Codex
   where it asks under Claude Code (M3). Recommendation: ask where the host
   can, deny otherwise (`agent-integration`: Ask before skipping). Settled by:
   the Codex hook documentation.
   Answer, 14 November 2026: no. The Codex hooks page
   (https://developers.openai.com/codex/hooks) says that
   `permissionDecision: "ask"` is "parsed but not supported yet"; Codex then
   marks the hook as failed and runs the tool call. `deny` with
   `permissionDecisionReason` is supported. So `cw guard --host codex` denies
   with the reason and tells the agent to ask the user, and
   `cw guard --host claude` asks. The page was read through a summarising
   fetch, so the quotation is close to the page and may not be exact. Nothing
   was run on Codex.
7. **Is Windows out of the first version?** The recommendation was yes. The
   owner decided no: Windows is in the first version (`platform-support`).
8. **Does a push judge only the tip commit?** Recommendation: yes
   (`publish-gate`: Push rule). The content that reaches the remote ref is
   checked, and a rebase stays cheap. The stricter option is to require every
   commit the push adds to be verified, which `git rebase --exec "cw run"`
   can produce but which makes every rebase cost one full run per commit.
   Settled by: the owner; whether the project needs every commit to pass, for
   example for `git bisect`.
9. **How does `cw` pass arguments to a batch file on Windows?** `npm`, `yarn`,
   `gradlew` and `mvn` are batch files there, and `cmd.exe` reads their
   command line by its own rules. Recommendation: pass each argument so that
   it arrives unchanged, and refuse a check with an argument that cannot be
   passed so (`platform-support`: Programs on Windows). Settled by: tests on
   Windows (task 4.7) and Go's documentation of `os/exec.Command`.
10. **What may a hook decision cost on Windows?** Starting a program and
    running git are slower there, and no number exists. Recommendation:
    measure in CI first, then set the limits in Success measure 2. Settled
    by: that measurement (task 10.1).
11. **How do Claude Code and Codex run a plugin's hook commands on Windows?**
    Today's hook files hold POSIX shell text. Recommendation: keep each hook
    command in a form the host runs on all three systems, and if a host
    offers none, state in the README that the plugin's two jobs are missing
    there. The git hooks do not depend on this. Settled by: each host's
    documentation and a test on Windows (tasks 8.1 and 8.2).
    Answer, 14 November 2026, from the documentation only; nothing was run
    on Windows. Claude Code (https://code.claude.com/docs/en/hooks) runs a
    hook command with `sh -c` on macOS and Linux and in Git Bash on Windows,
    and a hook can name `"shell": "bash"`. `cw` needs Git for Windows anyway,
    so the two hook commands stay POSIX shell text and name that shell. The
    Codex page does not say which shell runs a hook on Windows; it offers
    `commandWindows` as a Windows-only command. There the two commands are
    `cw context` and `cw guard --host codex`, which every shell can run. One
    job is therefore missing under Codex on Windows: the line that says `cw`
    is not installed. The README says so.
