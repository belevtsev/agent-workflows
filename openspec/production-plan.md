# Production plan: the evidence gate and the roles

Written on 14 November 2026. The owner asked for this plan, said that nobody
can guide the implementation, and wants something to test on return. This file
is the overview. The engineering content is in the two changes:

| Part | Proposal | Requirements | Design | Tasks |
| --- | --- | --- | --- | --- |
| 1. The gate | [proposal](changes/reduce-cw-to-evidence-gate/proposal.md) | 55, with 206 scenarios | [design](changes/reduce-cw-to-evidence-gate/design.md) | [64 tasks](changes/reduce-cw-to-evidence-gate/tasks.md) |
| 2. The roles | [proposal](changes/add-role-workflow/proposal.md) | 46, with 97 scenarios | [design](changes/add-role-workflow/design.md) | [27 tasks](changes/add-role-workflow/tasks.md) |

## Outcome

One tool, `cw`, for a developer in any language and any codebase:

1. **The gate.** A commit or a push is allowed only when every check the
   repository declares in `cw.toml` has a recorded passing run on exactly that
   content. Git hooks enforce it for people and for agents alike.
2. **The roles.** A task goes from a specification to tested code through
   seven roles, each with one job, its own model and a limit on what it may
   change. The change folder records what was decided, and `cw` checks it.
3. **The token count.** `cw tokens` says what a task cost, role by role, and
   how much of it the workflow itself used.
4. **The skill.** `skills/cw/` tells a coding agent how to work with the gate
   for any task, with seven examples that a test replays against `cw`.

What users report about other tools, and what `cw` did about each finding, is
in [docs/other-tools.md](../docs/other-tools.md). Three of those findings speak
against parts of this plan, and the page says so first.

## Where the work happens

- Worktree `.claude/worktrees/evidence-gate`, branch
  `feat/evidence-gate-and-roles`, cut from `origin/main` at `3cbfb0d`.
- `main`, the owner's installed `cw` and the owner's plugin are not touched.
- Today's workflow is kept under the local branch name `workflow-v3`.

## What is decided without the owner

Each of these is the recommended answer of a proposal, taken because the owner
asked for the work to go on alone. Each can be undone.

- The three open questions of part 1 (9 to 11) and the twelve of part 2.
- Module path `github.com/belevtsev/agent-workflows/v3`; no tag.
- The cut happens on the branch, not on `main`.
- Local commits on the branch, with the identity earlier local commits used.
- The standard `flag` package in place of cobra, so that `cw` links no network
  code of its own. On Windows the system-call library links the package `net`.

## What is left for the owner

- Merging pull request 219, a tag, a release, and every repository setting.
  The branch was pushed and the draft pull request opened on the owner's word.
- Removing `_quarantine/` (task 11.1 of part 1).
- Running anything with a model: the evaluation and the comparison with one
  agent (part 2, group 7). Until then no document claims that the roles
  improve anything.
- The size limit of the gate's code. It holds 6,000 of 6,000 lines: 5,998 after
  the fixes of the second challenge and the removal of options nothing used,
  and two more in the test harness `internal/cwtest` for the fault F8
  (`go test ./internal/repocheck/ -run Size -v` prints the numbers). The next
  change to the gate needs a cut elsewhere, or the owner's word on the limit.
- Confirming the behaviour by hand on macOS, on Windows and on Codex. The
  branch is pushed, and pull request 219 is a draft. CI run 19, for commit
  `3eaa40e`, passed on Linux, macOS and Windows. Nothing was run inside Claude
  Code or Codex, and no model evaluation was run.

Tasks that stay open on 14 November 2026, by what they wait for:

| Waits for | Part 1 | Part 2 |
| --- | --- | --- |
| The owner's decision or account | 1.1, 1.2, 1.3, 11.1, 11.3, 11.4, 12.1, 12.2 | 7.1, 7.2, 7.3 |
| The first release (task 11.4) | 11.2, for the last part of its check: the attestation and the SBOM of a release; 11.5 | |
| The owner's clone | 10.4 | |

53 of the 64 tasks of part 1 and 24 of the 27 of part 2 are ticked: each one's
check passed as it is written. Where the check is a run on the three systems,
that is CI run 19 of pull request 219, for commit `3eaa40e`. Such a run uses the
race detector on Linux only and skips the test that waits ten minutes. Nine
checks name `go test -race ./...` on the three systems (tasks 3.4, 5.3, 5.4,
5.5, 6.9, 6.10, 6.11, 7.4 and 8.4): for them it is run 43638791541 of the workflow
CI, started by hand on the same commit with the input `thorough`, which uses
the race detector on macOS and Windows too and sets `CW_SLOW_TESTS=1`. In that run the ten-minute test passed on each system: its package took 602 s on Linux, 607 s on macOS and 632 s on Windows.
On Windows the tests skip what needs a POSIX shell, POSIX
file modes or a POSIX signal; `rg -n "t.Skip" internal scenarios` lists each
skip with its reason. Of task 11.2 the test of the deciding step passes, and
a local snapshot build with goreleaser 2.18.3 made six archives and a
`SHA256SUMS` that lists each; the SBOM step was left out there, because syft
is not installed on this machine.

## Milestones

Each ends in a commit and in something that can be tried.

| # | Milestone | Tasks | Done when | What can be tried |
| --- | --- | --- | --- | --- |
| M1 | Plan and specifications are in git | part 1: 1.4 | The two change folders and this plan are committed on the branch | Read them |
| M2 | Skeleton | part 1: group 2 | Every file with the verdict Move or Cut is under `_quarantine/`; `go build ./...` and `go test ./...` pass; `cw version` runs | `cw version` |
| M3 | Runs and status | part 1: groups 3 to 5 | `cw init`, `cw add`, `cw run`, `cw status` behave as their scenarios say | The first four of the five commands |
| M4 | The gate | part 1: groups 6 and 7 | The hooks deny and allow as their scenarios say; `cw stats`, `cw context`, `cw uninstall` work | A gated commit and a denied one, in a new repository |
| M5 | Plugin and platforms | part 1: group 8, tasks 3.4, 4.7 | The plugin's two jobs work on Claude Code's hook input; the code builds for macOS and Windows | The plugin in Claude Code |
| M6 | Roles | part 2: groups 2 and 3 | `cw roles`, `cw roles check`, `cw change status`, `cw change check` behave as their scenarios say; the role texts exist | The four commands; the texts |
| M7 | Tokens | part 2: group 4 | `cw tokens` prints the known counts for recorded sessions | `cw tokens` on a real session |
| M8 | Documents and self-use | part 1: groups 9 and 10; part 2: groups 5 and 6 | README, quick start, the test guide; this repository gates itself with `cw.toml` | Everything, by following `docs/try-it.md` |
| M9 | Review against other tools, and the usage skill | part 1: 8.5, 8.6 | The findings of the review each have an outcome; the skill's examples are replayed by a test; the guard reads a command as words; the token readers are compared with real records | `docs/other-tools.md`, `skills/cw/`, and part 10 of `docs/try-it.md` |
| M10 | Independent challenge, and the three systems | part 1: 1.4, 2.4, 3.4, 3.5, 4.4, 4.7, 6.2, 10.1 and the Tests tasks of groups 5 to 8 | Every finding of the advisers has an outcome; CI passes on Linux, macOS and Windows | The checks of pull request 219 |

Order inside a milestone follows the task lists. A scenario that proves wrong
is corrected in the specification first, and the correction is listed below
under "Changes to the specifications during the build".

## How the result is checked

| Check | Command | Runs |
| --- | --- | --- |
| Build | `go build ./...` | Linux |
| Tests, with the race detector | `go test -race ./...` | Linux |
| Other systems compile | `GOOS=darwin go vet ./...`, `GOOS=windows go vet ./...` | Linux |
| Lint | `golangci-lint run` | Linux |
| Known vulnerabilities | `govulncheck ./...` | Linux; it reads its database from the network |
| The specifications and task lists | `cw change check` once it exists; before that the scripts under `reports/` | Linux |
| The five commands | The quick start test, and by hand in a new repository | Linux |

The tests for the scenarios are written from the specifications by another
agent than the one that writes the code, as the task lists require. A test
that disagrees with the code is settled against the specification.

## Stop conditions

The work stops and waits for the owner when:

- a step would need a push, a tag, a deletion, or a change outside the
  worktree;
- a requirement cannot be met as written and no smaller correction keeps its
  purpose;
- a check of the repository cannot be made to pass without weakening it.

## Rollback

Everything is on one branch, `feat/evidence-gate-and-roles`, in one worktree.
The branch is pushed, and the draft pull request 219 shows it. `main` is
unchanged, and no tag, release or repository setting was touched. Closing the
pull request and removing the branch, here and on GitHub, and the worktree
restores the state before this work.

## Independent challenge

A reviewer that had not written the designs read both and the specifications
they rest on. Its findings and what was done about each:

It reported 12 findings on 14 November 2026, each with an experiment on git
2.53.0 or a quotation from the designs. None is deferred.

| # | Finding | Severity | Disposition | What was done |
| --- | --- | --- | --- | --- |
| 1 | A copy of the index gets a new modification time, so git trusts a file that was staged and edited in the same second. A pass could be recorded for content that was never checked. | blocker | resolved | The copy keeps the time of the original. Design, Git, Snapshot; a test edits a file in the second it was staged. |
| 2 | The run that decides was chosen by the clock. A clock set back lets an earlier pass outrank a later failure, and a pass dated in the future never ages out. | blocker | resolved | Finished records carry a sequence number given under a lock; a record dated in the future does not count. `check-evidence`: Satisfied check, Old runs; two new scenarios. |
| 3 | A submodule or nested repository with uncommitted edits is invisible to the snapshot, so a check runs on content no tree records. | blocker | resolved | `cw run` and `cw status` refuse such a working tree. `check-evidence`: Working tree that cannot be recorded; one new scenario. |
| 4 | A damaged record of a failure was skipped, so an earlier pass decided; nothing was synced to disk; a hook read every record of a check. | major | resolved | Records are synced before the rename; one directory per checked content; an unreadable record there makes the check not satisfied until it runs again. `check-evidence`: Damaged and concurrent records; one new scenario. |
| 5 | `cw run --base` had no design. | major | resolved | Design, Records, `--base`. |
| 6 | The rule for tags trusts remote-tracking refs, which a local process can write. | major | rejected against evidence | The forgery needs a deliberate write into the git directory. The same process can write a passing record or remove a hook, so closing this one path buys nothing, and the rule is what lets a tag on a published commit through (scenario "A tag on a commit the remote already has"). The boundary is stated: design, What the gate trusts; `SECURITY.md`. |
| 7 | Changes inside the git directory that turn the gate off are reported nowhere: `core.hooksPath`, a hand-written record, the mode file, `.git/info/exclude`. | major | resolved in part, the rest rejected against evidence | Resolved: `cw status` and `cw context` report hooks that git does not run (`gate-reporting`: Gate state, two new scenarios), `cw roles check` repeats that warning, and the mode is observe only for the exact word. Rejected: detecting a forged record or an exclude file, for the reason of finding 6. |
| 8 | `cw change status` had no stage for the commit, so it said `done` before the commit existed. | major | resolved | A `commit` stage, held while the change folder differs from `HEAD`. Settled in the role change when its code is written (M6). |
| 9 | The values of the review and approval tables were never listed, and an approval stayed valid after the approved file changed. | major | resolved | The values are fixed, and each review and approval row carries the content id it was given for. Settled in the role change (M6). |
| 10 | For an annotated tag `pre-push` receives the id of the tag object, and a tag can name a tree. | minor | resolved | The id is peeled to a commit; a tree is judged as a tree; anything else is denied. `publish-gate`: Push rule; one new scenario. |
| 11 | A reader could see an unfinished record before its lock was taken; the Windows lock did not exist. | minor | resolved | The lock is taken before the record is written and probed shared; `fslock_windows.go` is written (compiled, not run). |
| 12 | The content key needed framing for paths with newlines; `git write-tree` rewrote the index file, against "status changes nothing". | minor | resolved | The key hashes the raw NUL-ended entries; the tree of the index is written from a copy. `gate-reporting`: Status; one new scenario. |

The reviewer also confirmed by experiment: the tree inside `pre-commit` for a
plain commit, `commit -a` and `commit <path>`; the snapshot from a copied
hook index; hooks in a linked worktree; `pre-merge-commit`; the hooks that a
continued cherry-pick, a revert and a rebase run; `pre-push` on a dry run and
for a URL; that a Claude Code transcript repeats a message id with the same
usage; and the arithmetic of the token fixture.

### The second challenge: eight advisers

Eight advisers that had not written the code each took one view of it on 14 November 2026:
break the gate (G, ran on Linux with git 2.53), security (S), Windows and macOS (P, read and
cross-built), architecture and tests (A), the Claude Code formats against its documentation (C),
the Codex formats against the source of rust-v0.168.0 (X), a first-time user (U, ran), and the
role texts (R). They reported 93 findings. Each has one outcome:

- **resolved**: the code, a test or a text was changed.
- **resolved in documents**: the finding is true and cannot be closed in the code; it is now a
  stated limit, with a test of the git behaviour where one is possible.
- **rejected against evidence**: the evidence is named.
- **deferred**: changes no behaviour, fails no gate and loses no rule.

#### G: break the gate

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| G1 | A file that another process stages while `pre-commit` runs is committed without being judged: git reads the index again after the hook. | resolved in documents | A hook cannot hold the index. Stated in the Known limits and in `docs/hooks.md`; `scenarios/gitfacts` holds the git behaviour as a test. The push hook judges the tip that is pushed, so such a commit is still denied there. |
| G2 | A submodule could hide content: a path inside it marked "assume unchanged", or a submodule that is not checked out whose folder holds plain files. | resolved | `cw run` and `cw status` refuse both (`internal/gitx/snapshot.go`). |
| G3 | The content key of a check with `paths` ignored submodule entries and the content behind a link. | resolved; the link part in documents | Every submodule entry counts in the key of every check with paths. A link is recorded as a link, as git records it; `docs/evidence.md` says that `paths` is a promise about what a check reads. |
| G4 | A forced push that moves a branch back publishes a commit nobody judged. | rejected against evidence | The specification requires it: `publish-gate`, "Updates that add nothing", scenario "A branch is moved back". The remote already holds the commit. The consequence is stated in `docs/hooks.md`. |
| G5 | Refs without `cw.toml` could not be pushed: `refs/notes/`, an orphan branch, an old tag on a first push. | resolved | Notes are not judged. A ref that is no branch adds nothing when a branch of the same push contains its commit. An orphan branch needs a `cw.toml` that holds `version = 1`; the denial names the section of `docs/operations.md` that says so. |
| G6 | `docs/evidence.md` said that the working tree did not change while a check ran; only the snapshots before and after are compared. | resolved | The sentence says what is compared. |
| G7 | `git am` and the plumbing commands make commits without `pre-commit`. | resolved in documents | Stated in the Known limits and `docs/hooks.md`, with tests in `scenarios/gitfacts`. The push hook judges the tip. |
| G8 | A hook that names `cw hook pre-commit` only in a comment counted as installed. | resolved | Only a line that is no comment counts (`internal/gate/hookfiles.go`). |
| G9 | `paths = ["src"]` names a folder, matches nothing, and one pass then counted for ever. | resolved | Such a pattern is an invalid declaration; the message gives the pattern to write. |
| G10 | A file name with a line break could add false lines to a denial. | resolved | A name with a control character is printed quoted (`decl.Plain`). |

#### S: security

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| S1 | With an empty or relative entry in `PATH`, a hook ran `./cw` of the repository. | resolved | The hook files and every plugin command look for `cw` only in the entries of `PATH` that are absolute paths (see F6 below for the first fix, which did not hold on macOS); the Windows command of Codex tells `cmd.exe` not to search the working directory. |
| S2 | A pattern with many `**` took exponential time. | resolved | The matcher is linear; a test compares it with the recursive meaning. |
| S3 | The documents called CI a backstop against a weakened `cw.toml`. | resolved | `SECURITY.md` and `docs/operations.md` say that CI runs what the pushed `cw.toml` declares, and that the file is protected by review. |
| S4 | The guard missed `cw uninstall`, `cw init --observe`, an edit of `.git/config`, the wrappers `timeout`, `exec`, `nohup`, `eval`, a link to `cw.toml`, a write into the store through the shell, and another repository named by an absolute path. | resolved; two cases in documents | All but the last two are asked about. A redirection in a shell command and a file of another repository are listed as misses in `docs/operations.md`: the guard is a reminder, the git hooks decide. |
| S5 | `cw uninstall` removed any hook file that held cw's marker, also a tracked file in a `core.hooksPath` directory. | resolved | There it removes only a file that is exactly cw's script. |
| S6 | "cw starts no connection" was false in a partial clone, where git fetches a missing object. | resolved | Every git call of cw sets `GIT_NO_LAZY_FETCH=1`; a test in `scenarios/gitfacts`. |
| S7 | Keys, patterns and names from `cw.toml` were printed raw, with terminal control sequences. | resolved | Quoted, as in G10. |
| S8 | The guard allowed a call whose notice it could not read: larger than 16 MiB, a command given as a list, a patch under `input`. | resolved; one case rejected against evidence | The notice is decoded in one pass with no limit, and a list is read. Rejected: the source of Codex gives a patch in `tool_input.command` (`apply_patch.rs`), not under `input`. |
| S9 | The same as G7. | resolved in documents | See G7. |
| S10 | Logs followed the umask, and the store's folders were readable by others. | resolved | Logs, records and decisions are created `0600`, the folders `0700`. |
| S11 | `cw change check` followed a test path of `tests.md` out of the repository. | resolved | Refused (`internal/change/rules.go`). |

#### P: Windows and macOS

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| P1 | Ctrl+C reached the check's program as well; the run was recorded as failed. | resolved | The program starts in a process group of its own; a run that ends with the console's exit status, or fails while cw is being cancelled, is interrupted. |
| P2 | The rule for batch files was skipped when `cw run` started in a subfolder. | resolved | A program given as a path is read from the repository root; a name without a folder is found on `PATH` only. |
| P3 | The guard compared paths as text: another case, a trailing dot, a short name. | resolved | It compares files as the file system sees them, with every folder above. |
| P4 | A record could not be replaced while a reader had it open; the outcome was lost. | resolved | The rename and the read are repeated for a moment on Windows. |
| P5 | `cmd.exe` looked a batch file up again, the working directory first. | resolved | The check is started by the full path cw found. |
| P6 | A check id such as `aux` or `nul` is a device name on Windows. | resolved | Refused on every system. |
| P7 | Lock files opened with `CreateFile` skip the long-path handling of Go. | rejected against evidence | The Go runtime turns long paths on for the whole process (`runtime/os_windows.go`, `initLongPathSupport`, Go 1.27). |
| P8 | With `core.fsmonitor`, each git call of cw started a daemon that ended with the call. | resolved | cw calls git with `-c core.fsmonitor=false`. |

#### A: architecture, code and tests

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| A1 | The guard did not cover the hooks of a `core.hooksPath` directory. | resolved | With P3. |
| A2 | The guard missed other spellings of a name and had a size limit. | resolved | With P3 and S8. |
| A3 | No test could fail on what the log keeps. | resolved | `internal/runner/log_test.go`. |
| A4 | The role names were written in five places. | resolved | The token reader asks `internal/roles`; a test holds the rest together. |
| A5 | `internal/proc` held options nothing used. | resolved | Removed, and one more in `internal/fslock`. |
| A6 | The clean-up could delete a lock file that was held. | resolved | Lock files stay. |
| A7 | A run stayed open for ever when its log could not be created. | resolved | It is recorded as interrupted, with the cause. |
| A8 | Every failure of git was read as "absent". | resolved | Only exit status 1 means that; any other failure, and output past the limit, is an error. |
| A9 | The age at which a run stops deciding borrowed the constant of the logs. | resolved | A named constant says that the two are equal on purpose. |
| A10 | "The first 20 paths that differ" was written three times. | resolved | One function, `gitx.DiffPaths`. |
| A11 | Three copies of "write a file whole"; `cw add` did not sync. | resolved; one copy kept against the design | `cw add` and the store share one synced writer. The token store keeps its own: by the design, `internal/tokens` imports no package of the gate, and a damaged token file is reported. |
| A12 | The size test counts the test harness (`internal/cwtest`, 332 lines) and skipped outside a checkout. | resolved in part; the rule deferred | The test fails in CI when git cannot list the files. Which files count is the owner's rule. |
| A13 | A read error in the decisions file was ignored. | resolved | Counted as unreadable. |
| A14 | Three concurrency tests did not test what they named. | resolved | Rewritten: runs that overlap, no wall clock, no signal to a stored process id. |

#### C: Claude Code formats

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| C1 | An older `cw` on `PATH` exits 2 for `cw guard`, which blocks every tool call. | resolved | Every plugin command ends with exit status 0. |
| C2 | The hook at the end of a session has 1.5 seconds, whatever its `timeout` says. | resolved | Measured: 0.12 s for a record of 50 MB, 1.15 s for 500 MB. `docs/tokens.md` names the limit and the variable that raises it. |
| C3 | The `PowerShell` tool was not matched. | resolved | Matched, and read like a shell command. |
| C4 | The token reader depends on a record format the host does not document. | resolved | Said in `docs/tokens.md`. |
| C5 | A count taken when a role stops can miss its last turn. | resolved | Every later notice of the session reads again the records that grew. |
| C6 | Internal agents without a type produced unlabelled starts. | resolved | Ignored. |
| C7 | `cw context` and `cw tokens record` ignored the `cwd` of the notice. | resolved | Both use it; `cw context --host` reads the notice. |
| C8 | `MultiEdit` is not a current tool. | rejected against evidence | Older releases have it, and a name that never matches does no harm. |
| C9 | On some providers a role's effort is lowered. | resolved | One sentence in `docs/workflow.md`. |
| C10 | An edit made through the shell is not guarded; the description said otherwise. | resolved | The description names the edit tools; the misses are listed in `docs/operations.md`. |

#### X: Codex formats

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| X1 | The plugin's hooks do nothing until Codex trusts them; the README did not say how. | resolved | `README.md`. |
| X2 | Exit status 2 blocks every shell call and patch. | resolved | With C1; the Windows command ends with `exit /b 0`. |
| X3 | A patch given to the shell skipped the guard. | resolved; one case in documents | The shell command is read for a patch. `write_stdin` is listed as a miss. |
| X4 | The notice arrives before the record is complete. | resolved | With C5. |
| X5 | One overflow of the context window made the count of a thread unknown. | resolved | A total that falls is a reset; the total before it is carried. |
| X6 | A start that copies the parent's context is counted and labelled wrongly. | resolved | The coordinator's text forbids such a start. |
| X7 | The commands are POSIX text but run in the user's shell. | resolved | Wrapped in `sh -c`. |
| X8 | The README said "asks" for both hosts; Codex denies. | resolved | `README.md`. |
| X9 | The head of `skills/cw/SKILL.md` was not valid YAML. | resolved | Quoted; a test holds the rule. |
| X10 | On Windows a missing `cw` showed a failed hook. | resolved | With X2. |

#### U: a first-time user

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| U1 | After a project removed `cw.toml`, every commit of a teammate was denied, and the advice was `cw init`. | resolved | The denial names the section "A tree without cw.toml" of `docs/operations.md`. It names no command, as the specification of the denial requires. |
| U2 | `source_changed` came without advice, and the denial dropped the cause. | resolved | The run says what to do about files a check writes; the denial names the last run that decided nothing. |
| U3 | After a failed test the denial said only "run `cw run`". | resolved | It says to fix what the log shows first; a failed `cw run` ends with one line. |
| U4 | "Set it aside" never said how. | resolved | The message names `git add -A` and `git stash push --keep-index --include-untracked`. |
| U5 | A denied push sent the user to a linked worktree when `cw run` here was enough. | resolved | Only when the working tree holds other checked content for a missing check. |
| U6 | In a clone without hooks, `cw status` and `cw context` contradicted git. | resolved | Both say first that no hook judges here yet. |
| U7 | No way to replace a check. | resolved by a hint; a new command rejected | `cw add` says that it only appends and that `cw.toml` is edited by hand. A command to edit is rejected: the file is the interface (`check-declaration`). |
| U8 | Forgetting `--` in `cw add` gave a bare error. | resolved | A hint. |
| U9 | `cw add unit -- "go test ./..."` was accepted and failed at the run. | resolved | Refused, with the form to write. |
| U10 | `--help` describes no option. | resolved | Deferred at first as a text only. `cw <command> --help` now prints one line for each argument and option of the command; `TestHelpOfACommandExplainsEveryWordOfItsUsageLine` holds that every word of the usage line is explained (commit `f418250`). |
| U11 | The install part of the README was incomplete. | resolved | `README.md`. |
| U12 | `cw init` in a repository with checks printed the wrong next step. | resolved | It prints `next: cw run`. |
| U13 | Notices used Go's list syntax. | resolved | Lists are printed as `cw.toml` holds them; an absent `paths` is "not set". |
| U14 | `docs/try-it.md` showed less than cw prints; the uninstall message named one place for two things. | resolved | Both. |
| U15 | `cw stats` counts a commit with nothing to commit as allowed; the table of `cw roles` is 200 columns wide. | resolved in documents; the table resolved | `docs/operations.md` says what "allowed" counts. The table was deferred at first as a matter of looks; it was 253 columns wide. `cw roles` now prints the job of a role and what it may write below its row, and `TestTheTableFitsATerminal` holds every line to 120 columns. |

#### R: the role texts

| # | Finding | Outcome | What was done |
| --- | --- | --- | --- |
| R1 | Nobody owned an existing test that a change breaks. | resolved | The quality engineer does; the developer's text says so. |
| R2 | A wrong test had no way back. | resolved | The developer returns it to the quality engineer with the reason. |
| R3 | Open questions could reach a review. | resolved | Settled before the review starts. |
| R4 | Only the developer could reject a blocker; a "Checked" column and a token tie were proposed. | resolved; two proposals rejected | The rule is in the texts. The column and the tie add work without a failure they prevent. |
| R5 | A task was ticked before its review. | resolved | After it. |
| R6 | `cw change check` ran only at the end. | resolved | After each role that writes the record. |
| R7 | The texts assumed a language without a build step. | resolved | They name the build where a language has one. |
| R8 | The two paths were defined in two places. | resolved | In one. |
| R9 | The small path had no rule. | resolved | Stated plainly: no record, the gate still decides. |
| R10 | A task could start on a working tree with other changes. | resolved | The coordinator checks first. |
| R11 | The models of the roles. | resolved; one proposal rejected | The reviewer runs at the architect's model and effort. Cheaper models for the spec-writer and the developer are rejected: the owner asked for strong models in the roles that decide quality. |
| R12 | Roles repeat each other's reading. | deferred | Costs tokens only; the token report shows it. |
| R13 | The `git` role costs a start for one commit. | rejected against evidence | The owner named the role. |
| R14 | One developer start for all groups of tasks. | resolved | One start for each group. |
| R15 | The meaning of a stage was unclear. | resolved | Defined in the coordinator's text and in `docs/workflow.md`. |

#### Totals

93 findings. By row: 84 resolved, 4 resolved in documents (G1, G7, S9, U15), 4 rejected against
evidence (G4, P7, C8, R13) and 1 deferred (R12). Ten rows have a part with another outcome,
named in the row: in documents for G3, S4 and X3; rejected for S8, A11, U7, R4 and R11; deferred
for A12; resolved in the code for U15. U10 and the table of U15 were deferred when the challenge
closed and were resolved afterwards, as their rows say.

#### Found while the fixes were tested

Three helpers then wrote the tests, the scenarios and the documents for these fixes without
having written the code. They and their tests found six more defects, and a seventh (F7) was found by reading the
Windows commands again after F6. An eighth (F8) is a fault of the tests themselves, found by
running them sixty times in a row. A ninth (F9) was found by the first thorough CI run (L15
below). Each is resolved:

| # | Defect | What was done |
| --- | --- | --- |
| F1 | On Windows a check named by a bare file name was refused when the repository root held a file of that name: Go refuses a program it finds in the working directory. | A name without a folder is found on `PATH` only, as on the other systems; `platform-support`, scenario "A batch file of the same name in the repository". |
| F2 | Under Codex on Windows the start of a session printed no line when `cw` is not installed, against `agent-integration`, Instruction text comes from `cw`. | The Windows command prints the line in a repository that holds `cw.toml`; `platform-support`, scenario "Codex starts a session on Windows without `cw`". |
| F3 | `cw change check` failed on this repository: no task of part 2 named the requirement "Notices as the host gives them", which the fixes had added. | Task 4.2 of part 2 names it. |
| F4 | `cw tokens record` created its folders `0755`, against finding S10. | They are created `0700`; `internal/tokens`, `TestRecordKeepsItsFoldersPrivate`. |
| F5 | For a copy of `cw`'s own hook file that was changed since, `cw uninstall` printed a line the file does not hold. | It prints the file's own line that calls `cw`; `internal/gate`, `TestUninstallNamesTheLineTheFileHolds`. |
| F6 | The fix for S1 did not hold on macOS. It ran `cw` only when `command -v cw` printed an absolute path, and the `sh` of macOS (bash 3.2 in POSIX mode) prints one also for a file found through an empty or relative entry. Found by the new tests in the first CI run that reached them on macOS. | The hook file and the plugin commands leave every entry of `PATH` that is not an absolute path out before they look. Tests run the hook file and the plugin commands in bash started as `sh`, which behaves as on macOS, so Linux holds the rule too; `internal/gate`, `TestScriptRunsNoCWOfAnEmptyOrRelativeEntryOfPATH`. |
| F7 | Two more ways to a program of the repository through a relative entry of `PATH`, of the same kind as F6. At the start of a session without `cw` the plugin commands ran `git` with the whole `PATH`. Under Codex on Windows `cmd.exe` searched a relative entry, such as `.`, from the session's directory. | The commands for a POSIX shell keep only the absolute entries of `PATH` for all they run. The Windows commands go to the Windows folder before they look for `cw`, and without `cw` they run no program: the line that says `cw` is not installed is printed when the session's directory itself holds `cw.toml`, no longer from a subfolder. `agent-integration`, scenario "No absolute entry in `PATH`"; `platform-support`, scenario "A relative entry of `PATH` under Codex on Windows". |
| F8 | A test failed once in sixty repeats of the scenario tests under load, on Linux (`go test -race -count=60 -timeout 25m ./scenarios/...`): `TestSessionContextComesFromTheInstalledCW` wrote a small program and ran it, and the system refused with "text file busy". Another test of the same process had started a program while the file was open for writing, and its child held the file open. Every test that writes a program and runs it while other tests of its package run had the same fault. | The tests write such a file through one helper, `writeProgram`, which lets no other test start a program while the file is open (`syscall.ForkLock`). Nine packages use it. Eight need it; the tests of `internal/gate` do not run in parallel and use it only so that every test writes such a file the same way. The harness does the same when it copies a program named by `CW_TEST_BINARY`. The sixty repeats then passed, with `./internal/gate/` and `./internal/repocheck/` added to the command, and passed again on the last tree. Before the change one hundred repeats of `./scenarios/plugin/` and `./internal/gate/` had passed, which shows how rare the fault was. All of these runs were local, on Linux. It is a fault of the tests, not of `cw`. |
| F9 | The first thorough CI run (43637283601, commit `c23bb3e`) failed on Windows, with the race detector: `TestRunLeavesNoChildAfterTheProgramExits` found the child of a program still running after `proc.Run` returned. Two calls gave Windows the address of a structure as a number: the limit that ends a job's processes when its handle closes, and the list of the job's processes, 8 KB large. Both structures lived on the stack of the goroutine: `go build -gcflags=-m` named neither as moved to the heap. Go may move a stack when it grows and knows no pointer in a number. When it moved between the conversion and the system call, Windows wrote the list to the old place: `Run` saw no process, waited for none, and returned while the processes of the check were still ending. | Both calls go through `Call` of the DLL, which moves a structure whose address is passed to it to the heap and keeps it alive; the compiler now reports "moved to heap" for both (`internal/proc/group_windows.go`). The thorough run 43638791541, on commit `3eaa40e`, passed on Windows. The fault showed only with the race detector, which makes every stack frame larger; whether a stack moved there in a plain build was not measured. |

#### A last review of the plugin commands

After F7 a ninth adviser, who had not written them, read the hook commands of both plugin files
as each shell reads them, and ran the commands for a POSIX shell with `sh`, `bash` and `bash`
started as `sh`. It could run nothing on Windows. It reported five findings and found no way to
break the commands for a POSIX shell. Each of the five is resolved. H6 came from the test
written for H4 and is rejected against evidence:

| # | Finding | What was done |
| --- | --- | --- |
| H1 | Under Codex on Windows an entry of `PATH` such as `D:tools` still led into the repository when the session is on another drive than the Windows folder: `cmd.exe` keeps the directory it left on each drive. Reasoned from documentation, not run. | Each Windows command goes to the root of the session's drive before it goes to the Windows folder. `platform-support`, scenario "A drive-relative entry of `PATH` under Codex on Windows"; the test gives the repository a drive letter with `subst` and first shows that the earlier form of the command ran the planted file. |
| H2 | The description in `hooks/codex-hooks.json` said `cw` runs "only from an entry of `PATH` that leads outside the working directory". The commands for a POSIX shell take every absolute entry, also one that points into the repository. | The description says "an entry of `PATH` that is an absolute path". An absolute entry that points into the repository is the user's own setting and stays outside the rule. |
| H3 | At the start of a session the line that says `cw` is not installed was missing when the `cw` found cannot be run: a file that is not executable under `bash`, and on Windows a file `cw` without an extension, which `where` finds and `cmd.exe` does not run. | The commands for a POSIX shell take a file that is not executable as no `cw`. The Windows command no longer asks `where`: it runs `cw context` and prints the line when `cmd.exe` found nothing to run. `agent-integration`, scenario "A `cw` that cannot be run"; `platform-support`, scenario "A file named `cw` that Windows cannot run". |
| H4 | No test showed that the installed `cw` still runs when a relative entry comes first in `PATH`. | `agent-integration`, scenario "A relative entry before the installed `cw`"; on Windows the scenario "A relative entry of `PATH` under Codex on Windows" now also has the installed `cw` in a later entry. |
| H5 | No test and no scenario covered H1. | The scenario and test named under H1. |
| H6 | Found by the test for H4, in CI on Windows: with an empty first entry of `PATH` and the installed `cw` in a later one, the command before a tool call printed no decision. | Rejected as a defect of the command, against three CI runs of pull request 219. In run 11 (commit `3db1d6d`) the sub-test "PreToolUse with the installed cw" with `PATH=` failed both ways of starting `cmd.exe`: no decision was printed. From run 12 on (commit `0bb35b1`) the test also asks `cmd.exe` itself for `cw version` through that `PATH` from an empty folder, and passes only when the command prints the decision or that question finds no `cw`. From run 13 on (commit `d424e7d`) it first shows that the same question finds `cw` through the `PATH` without the empty entry. The command is the same in the three runs, and runs 12 and 13 passed. So through such a `PATH` `cmd.exe` itself runs no `cw`, and the command has none to run. Before a tool call it prints nothing; at the start of a session it prints the line that says `cw` is not installed (L1 below). `docs/hooks.md` says both. |

#### A walk through the guide

A tester that had not seen [the guide](../docs/try-it.md) then followed its parts 1 to 9 and 11
on Linux, with the program built from commit `3db1d6d` and a home folder of its own. Every
command worked, and every output, exit status and count the guide shows matched. It named six
places where the words were not exact or a line was missing, and each is corrected in the guide:

| # | What the tester found | What was done |
| --- | --- | --- |
| T1 | The guide makes commits and does not say that git needs a name and an e-mail. | "What you need" says so; the quick start says so too. |
| T2 | `cw add` prints a line the guide did not show. | Part 2 shows it. |
| T3 | `cw status --json` holds `"tree"` at the top and in each check; part 6 did not say which to take. | Part 6 names the first one. |
| T4 | Part 7 said that `cw change status` and `cw change check` only read files. `cw change status` writes temporary files in the git directory and removes them. | Part 7 says that they change no file of the checkout and leave nothing behind. |
| T5 | Part 8 said the first line of the token report counts "60,000 known tokens"; the word "known" is on the second line. | Part 8 says that the report counts them. |
| T6 | Part 9 said that `cw run` takes a few minutes; with a warm build cache it took ten seconds. | Part 9 says that it can take a few minutes the first time. |

#### A last review of the tests and the closing documents

A tenth adviser, who had written none of it, then read the two commits for F8 and the closing
documents against the repository, and counted every number these documents state. It found the
lock of F8 complete for programs the test process starts, no way for it to stall, and no place
left where a test writes a program and runs it beside parallel tests. The numbers matched: 55
requirements and 206 scenarios in part 1, 46 and 97 in part 2, 93 findings of the second
challenge with 83, 4, 4 and 2 by outcome, and 6,000 of 6,000 lines. It reported fourteen
findings, none in the code of `cw`. Each is resolved:

| # | Finding | What was done |
| --- | --- | --- |
| L1 | `docs/hooks.md` said that behind an empty first entry of `PATH` the Windows commands "do nothing". At the start of a session the command then prints the line that says `cw` is not installed, although it is installed. | `docs/hooks.md` says so. The test of the scenario "A relative entry of `PATH` under Codex on Windows" also runs the command at the start of a session with the installed `cw` in a later entry: it must print what `cw context` prints, or, where `cmd.exe` itself finds no `cw`, that line. |
| L2 | The row H6 and `docs/hooks.md` named no CI run, and the test passes on either branch, so one green run does not show which was taken. | The row H6 names runs 11, 12 and 13 and what each showed. `docs/hooks.md` says where and when it was seen. |
| L3 | The table of open tasks listed 3.5, 10.1 and 11.2 as work that was not done. Each is done. | The table under "What is left for the owner" is the state at the end. |
| L4 | The README, `docs/try-it.md` and this plan named CI run 4, thirteen commits back. | They name the run on the last commit that changes code or tests. |
| L5 | The description in `hooks/codex-hooks.json` said that on Windows `cw` runs "only from an entry that does not lead into the session's directory". An absolute entry may lead there, and the commands do not leave a relative entry out: they change where it is read from. | The description says what the commands do. Reading them again for this showed one case they do not cover: a session in the root of a drive, with an entry of `PATH` such as `X:tools` that names that drive. The requirement Hooks on Windows, the design and `docs/hooks.md` name it. |
| L6 | The pull request text and this plan said that every finding of the ninth adviser is resolved, and did not say that H6 is rejected. | Both say five resolved, and H6 rejected against evidence. |
| L7 | The row of M1 and the section Rollback still spoke of a local branch and of no pull request. | Both say that the branch is pushed and that the draft pull request 219 shows it. |
| L8 | The commit for F8 says "nine packages that need it". The tests of `internal/gate` do not run in parallel, so eight need it. | The row F8 says so. The commit message stays as it is: no commit is amended. |
| L9 | The repeats named under F8 had no command and no place. | The row F8 gives each command and says that the runs were local, on Linux. |
| L10 | This plan credited all 6,000 lines of the gate to the second challenge. | The plan says that the last two are the test harness, for F8. |
| L11 | The rows of the walk through the guide were named G1 to G6, as the findings of the first adviser are. | They are T1 to T6. |
| L12 | The guides said that `git config user.name` must print a value. A name given through git's environment prints none there. | The guides say to set the name and the e-mail when the command prints nothing. |
| L13 | "Every output matched the guide", while T2 is a line the guide did not show. | The sentence says that every output the guide shows matched. |
| L14 | `docs/hooks.md` said that a plugin command does nothing without `cw` "except at the start of a session". With no absolute entry in `PATH` it prints nothing there either. | `docs/hooks.md` says so. |

The adviser then checked each correction against the pushed state. It found L1 to L14 closed
and reported six more points, all in what the corrections had written. Each is resolved:

| # | Finding | What was done |
| --- | --- | --- |
| L15 | This plan said that each ticked task's check passed as it is written. Nine checks name `go test -race ./...` on the three systems; CI used the race detector on Linux only, and the test that waits ten minutes for the default timeout did not run in CI. | The CI workflow has an input `thorough` for a run started by hand: the race detector on every system, and `CW_SLOW_TESTS=1`. The first such run, 43637283601 on commit `c23bb3e`, failed on Windows and showed the defect F9. Run 43638791541, on commit `3eaa40e`, passed. The paragraph under the table of open tasks says which run proves which check. A run for a pull request is unchanged. |
| L16 | The answer to Open Question 9 said "from the tests on Windows" what no test showed: that a plain argument reaches a batch file unchanged, and that a program that is not a batch file gets every argument unchanged. It said "letters" for the letters a to z, and did not say that an empty argument is refused. | A test on Windows shows both (`TestPlainArgumentsReachABatchFileAndEveryArgumentReachesAProgram`), and `BatchRefusal` has a test of its own on every system. The answer names the letters and the empty argument. |
| L17 | The case the Windows commands of Codex do not cover was named as "a session in the root of a drive". It is wider and narrower: an entry such as `X:tools` is read from the root of drive X when the session is on that drive and the Windows folder is not, so it also reaches a repository that the entry names from the root; on the drive of the Windows folder it is read from the Windows folder. | The requirement, the design and `docs/hooks.md` say exactly that. |
| L18 | The requirement Hooks on Windows kept its "SHALL" beside a case that breaks it, and did not hold the limit of the empty first entry. | The requirement says that two exceptions follow, and its second paragraph holds both. |
| L19 | The new test of the command at the start of a session passed for any output that is not the line for a missing `cw`, and its control asked `cmd.exe` from another folder than the one the command runs `cw` in. | The test looks for the text of `cw context`, and the control asks from the Windows folder. |
| L20 | The rows of M2 and M3 still said that their tasks are not ticked, and the row of M10 left out task 6.2. | The rows say when the tasks were ticked, and M10 lists 6.2. |

## Changes to the specifications during the build

| Date | Where | What changed | Why |
| --- | --- | --- | --- |
| 14 Nov 2026 | `check-evidence`: Working tree that cannot be recorded | Also refuses a submodule with uncommitted changes; one scenario added | Challenge finding 3 |
| 14 Nov 2026 | `check-evidence`: Satisfied check, Old runs | Order of runs is the order of recording, not the clock; a run dated in the future does not count; two scenarios added | Challenge finding 2 |
| 14 Nov 2026 | `check-evidence`: Damaged and concurrent records | An unreadable record makes its check not satisfied for that content until it runs again; records are synced; one scenario added | Challenge finding 4 |
| 14 Nov 2026 | `publish-gate`: Push rule | One scenario added: an annotated tag | Challenge finding 10 |
| 14 Nov 2026 | `gate-reporting`: Status; new requirement Gate state | The index file is not rewritten (one scenario); `cw status` reports hooks git does not run (two scenarios) | Challenge findings 12 and 7 |
| 14 Nov 2026 | Part 1 totals | 54 requirements and 144 scenarios, from 53 and 136 | The rows above |
| 14 Nov 2026 | `publish-gate`: Hooks of other tools | `cw init` exits 4 only for a hook that does not call `cw hook <name>` yet; one scenario added. Part 1 now has 145 scenarios | A unit test showed that `cw init` exited 4 for ever in a repository whose shared hooks already called `cw`. The test of scenario "Another tool's hook still calls `cw`" expected that exit in its setup and was changed with the specification |
| 14 Nov 2026 | Design, Records | `pass_after_failure` compares runs of the same command; a base revision implies nothing for a check that has a decisive run of its own, however old; `--name-status`; a run without an end belongs to the period it started in | Two unit tests failed against the design (the code was fixed), and two sentences of the design did not say what the code does |
| 14 Nov 2026 | `agent-integration`, design: Plugin; proposal, Open Questions 6 and 11 | A tenth command, `cw guard`, holds the plugin's question before a tool call; Codex denies because it cannot ask | The hosts' documentation |
| 14 Nov 2026 | `check-declaration`: fixture, the meaning of "Refused" | "Writes nothing" now says what it covers: no new or changed file or directory in the working tree, the home directory or the git directory; objects git stores for a snapshot are not counted | Three scenario tests failed because a refused `cw run` and `cw status` left an empty `cw/` directory. The code was fixed; the wording was made exact |
| 14 Nov 2026 | `change-record`: new requirement Reviewed content; Stage from the files; fixture | Each review and each approval holds a content id and counts only for that content; a stage `commit` between `review` and `done`; the fixture is committed; six scenarios added | Challenge findings 8 and 9: an approval stayed valid after the approved file changed, and `done` was printed for a record nobody had committed |
| 14 Nov 2026 | `role-definitions`: Write limits | `cw roles check` warns, as `cw status` does, in a clone whose hooks git does not run; one scenario added. Part 2 now has 45 requirements and 89 scenarios | Challenge finding 7 |
| 14 Nov 2026 | `change-record`: Reviewed content | The `code` id covers every path outside `openspec/changes/`, not outside the one folder. A review of the code also counts for the last commit that changed the folder only when `HEAD` holds the folder as the working tree does. Two scenarios reworded: an edit of the code counts as a change after the review while the record is not committed, and as later work once it is | Found while building: as first written, the rule and its scenario "The code changes after its review" contradicted each other, and the id of one folder made a second task's record stale |
| 14 Nov 2026 | `role-definitions`: Role text names no language; Text budget | The search also covers the files generated for a host; one scenario added for the sizes `cw roles` prints. Part 2 now has 90 scenarios | A scenario test failed: the header of each generated role file named a Go command, and a host gives that file to a model. The header was changed. The test agent also found that the sentence about the sizes had no scenario |
| 14 Nov 2026 | `change-record`: three sentences | "says that it has no scenario"; "the file `tests/app_test.txt`"; "the line where the broken rule has one" | The test agent found each of them open to two readings. More rules now name a line: a requirement no task names, a scenario without a row, a cycle |
| 14 Nov 2026 | `change-record`: fixture | The texts of the two tests are `greets_by_name` and `greets_without_name` | Found while trying the scenario "A test is renamed" by hand: `no_name_is_given` holds `name_is_given`, so the renamed test was still found |
| 14 Nov 2026 | Part 2 design, Tokens: the notice | `cw tokens record` reads a fifth field of the notice, `agent_type`, and stores the role it names for a start whose record gives no label. An unknown start without a label is a start of that role in the one task of its session; in every other case each task of its session is incomplete and names the record | Two scenario tests failed ("A record that cannot be read", "The report says that it is incomplete"): with an empty record the label was lost with the counts, so the start was listed in no task and the task stayed `complete`. The specification was right and the design had a gap; the code and the design were changed |
| 14 Nov 2026 | Part 2 design, Tokens: a record of Codex | A `token_usage_record` without a `response_id` is a running total and is not counted; a record that holds only running totals holds no count | The scenario test "A Codex session" failed: the code counted such a line as one more request, so every start was counted twice. The code was fixed and the sentence made exact |
| 14 Nov 2026 | Part 2 design, Tokens: three sentences | Without `--host` the host is `claude`; the Codex hook at the end of a session has a timeout of 3 because Codex allows 1 to 3 seconds; with an unknown start the sentence of the report says "N of M known tokens" | The test agent found the first two unstated. The third was found by hand: a share of an incomplete total was printed as if it were the share of the whole |
| 14 Nov 2026 | Part 2 design | A block of 69 lines that repeated the sections from "The change folder" to "Tokens" was taken out | A broken edit of mine on 14 Nov 2026 left it there; the writer of `docs/evaluation.md` found it. No requirement and no scenario changed |
| 14 Nov 2026 | Both designs, Rules | "Checked by" names the checks `test` and `record` of `cw.toml` in place of the word "review". The rule "Only `internal/gitx` starts git" now says how it holds: a program is started only by `internal/proc`, which only `internal/gitx` and `internal/runner` import | Task 6.3 of part 2. Two rules had no test; `internal/repocheck` now has one for each |
| 14 Nov 2026 | Part 1 design: Declaration, Clean-up, Gate state, Plugin, `--base`; proposal, Known limits | Six sentences now say what the code does: a pattern with any empty segment is refused; the clean-up goes by the time a file was last written and also removes decision files and index copies; a hook counts only when it can be executed; the Codex command for Windows prints no line when `cw` is missing; the base implies nothing for a check with an unreadable or future-dated record; a working tree with uncommitted changes in a submodule is refused | The writer of `docs/operations.md`, `docs/hooks.md` and `docs/evidence.md` compared the design with the code and listed seven differences. In each the code was right or stricter. The seventh, a limit of 1,000 characters that nothing enforced, got a test |
| 14 Nov 2026 | Part 2 design: Tokens, `cw change status`, Failure behaviour | A role that ended without a record is stored with unknown tokens; a damaged stored file makes the tasks of its session incomplete; `cw change status` also prints the number of broken rules; `cw roles text` does not read `cw-roles.toml`; `cw tokens record` does not read the notice outside a gated repository | The writer of `docs/roles.md`, `docs/workflow.md` and `docs/tokens.md` found five cases with a concrete failure and six differences between design and code. The code was changed in seven places and the design in four |
| 14 Nov 2026 | Part 1, task 10.3 | "Leave no complexity exemption" now says "for code that is not a test" | The limits hold for every function that is not a test. Test files keep one exemption, with its reason in `.golangci.yml`: a test function is as long as its cases. Without it five test functions exceed a limit |
| 14 Nov 2026 | `agent-integration`: Ask before skipping; The plugin never judges a commit or push; design, Plugin; proposal, Known limits | The guard reads a shell command as words: it asks before `--no-verify`, `-n` of `git commit`, a setting of `core.hooksPath`, and an edit of a hook file or of a record. The same text inside an argument is no reason to ask. Five scenarios added. "Never judges" now says what it always meant: the plugin does not decide about content; it reads a command only to ask before a skip | Review of other tools, row G-2: the old rule asked before `grep -- --no-verify docs/hooks.md` and missed `git commit -n`. `internal/shellwords` is a new package of 151 lines; the package of that name that the first review criticised decided the gate, and this one decides nothing |
| 14 Nov 2026 | `agent-integration`: new requirement Usage skill | The plugin ships the skill `cw`; a test replays its examples and allows only commands `cw help` lists; no skill holds a `cw` version; four scenarios added. Part 1 now has 55 requirements, 154 scenarios and 64 tasks | The owner's request of 14 November 2026. The advice to record a tool version in the skill was rejected against the requirement "Instruction text comes from `cw`" |
| 14 Nov 2026 | `task-flow`: Two paths | For a change of a few lines with a plain cause the coordinator says that the task needs no roles; one scenario and the case E26 added. Part 2 now has 91 scenarios. The texts of `spec-writer` and `architect` say to write no more than the behaviour needs | Review of other tools, rows R-1 and R-2 |
| 14 Nov 2026 | Part 2 design, Tokens: a record of Codex, a record of Claude Code | Codex writes running totals in `token_count` events; the thread is counted once from them, and a total that starts above its own request is unknown; a total that falls is a reset, and the total before it is carried. The line `token_usage_record` that the first reader expected exists in no real record | The readers were run on real records: 170 of Claude Code 2.1, where the reader was right, and 16 of codex-cli 0.141 and 0.142, where it found no count |
| 14 Nov 2026 | Part 2, second challenge: `token-report`, `change-record`, `role-definitions`, `task-flow` | One requirement added, Notices as the host gives them, with three scenarios; three more scenarios added: "A Codex total that is reset", "A test file outside the repository" and "The quality engineer removes an existing test"; the texts of the roles say who owns an existing test and how a wrong test goes back. Part 2 now has 46 requirements and 97 scenarios | Second challenge, findings of the views C, X and R; commit `c5a88ef` |
| 14 Nov 2026 | Part 1, second challenge: all six specifications | 46 scenarios added, each for a behaviour that a fix of the second challenge changed: 6 in `check-declaration`, 4 in `check-evidence`, 15 in `publish-gate`, 1 in `gate-reporting`, 13 in `agent-integration` and 7 in `platform-support`. The texts of 20 requirements got a clause for what their new scenarios hold; no requirement was added and none lost a rule. Part 1 now has 55 requirements and 200 scenarios | Second challenge; the Source line of each requirement names its findings. The scenarios and their tests were written by helpers that had not written the fixes |
| 14 Nov 2026 | `platform-support`: Programs on Windows; part 1, task 4.7 | A program named without a folder is found in the folders of `PATH` only; a program given as a path is read from the repository root; the file is started by its full path. Before, the text said "as the system does" | Defect F1 above: Windows, and Go for it, look in the working directory first, which let a file of the repository stand in for a program of `PATH` or made Go refuse the check |
| 14 Nov 2026 | `publish-gate`: Removal | `cw uninstall` prints "the line of it that holds the call", not "the line to remove from it" | Defect F5 above |
| 14 Nov 2026 | Part 1 proposal, Open Question 11 | Under Codex on Windows the start of a session now prints the line that says `cw` is not installed; the row of this table that says it prints no line is out of date | Defect F2 above |
| 14 Nov 2026 | `publish-gate`: Fail closed; `agent-integration`: Instruction text comes from `cw`; part 1 design, Hooks and Plugin | The hook file and the plugin "look for `cw` only in the entries of `PATH` that are absolute paths". Before, they ran what `command -v cw` printed when that was an absolute path, and denied "a `cw` that `PATH` names by a relative path". One message now covers a `cw` that is not installed and one that only an empty or relative entry holds; the scenario "An empty or relative entry of `PATH`" expects it | Defect F6 above |
| 14 Nov 2026 | `agent-integration`: Instruction text comes from `cw`; `platform-support`: Hooks on Windows; part 1 design, Plugin | The plugin's commands for a POSIX shell "run `cw` and `git` only from the entries of `PATH` that are absolute paths"; the Windows commands of Codex "run no program of the working directory, also not through a relative entry of `PATH`". Two scenarios added: "No absolute entry in `PATH`" and "A relative entry of `PATH` under Codex on Windows". Under Codex on Windows the line for a missing `cw` is printed in the folder that holds `cw.toml` only | Defect F7 above |
| 14 Nov 2026 | `agent-integration`: Instruction text comes from `cw`; `platform-support`: Hooks on Windows | Four scenarios added: "A relative entry before the installed `cw`", "A `cw` that cannot be run", "A drive-relative entry of `PATH` under Codex on Windows" and "A file named `cw` that Windows cannot run". The scenario "A relative entry of `PATH` under Codex on Windows" also runs with the installed `cw` in a later entry | Findings H1, H3, H4 and H5 above |
| 14 Nov 2026 | `platform-support`: Hooks on Windows; part 1 design, Plugin | The requirement names two exceptions for the Windows commands of Codex: behind an empty first entry of `PATH` `cmd.exe` may run no `cw` of a later entry, and an entry such as `X:tools` is read from the root of drive X when the working directory is on that drive and the Windows folder is not. No scenario changed: "A drive-relative entry of `PATH` under Codex on Windows" already says "not the root" | Findings L5, L17 and L18 of the last review |

## Progress

| Milestone | State | Commit | Evidence |
| --- | --- | --- | --- |
| M1 | done | `4fff8ad` | The two change folders and this plan are on the branch. No pull request at that time; the branch was pushed and the draft pull request 219 opened later, on the owner's word. |
| M2 | done | `cb59433` | Local, Linux: `go build`, `go vet`, `go test -race` pass; 2,216 files hold the same blob under `_quarantine/`; `cw version` exits 0 and `cw nosuch` exits 2; `claude plugin validate .` passes. Task 2.4 was written but not ticked then: its check is a CI run. It is ticked at M10. |
| M3 | done, Linux only | `df3f3b5`, tests in the commit after it | Local, Linux: the 62 scenarios of tasks 5.3 to 5.5 each have a test, written from the specifications by agents that did not read the code. 58 pass; 3 need Windows and are skipped; 1 waits ten minutes and runs only with `CW_SLOW_TESTS=1`; it ran once, on 14 Nov 2026, and passed in 600 seconds. Three tests failed first (a refused command left an empty `cw/` directory); the code was fixed. Tasks 3.x to 5.x were not ticked then where their check names the three systems. They are ticked at M10. |
| M4 | done, Linux only | `9211f5b`, tests in `ba569f7` | Local, Linux, by hand in a demo repository: an unchecked commit is denied and a checked one recorded; a commit that leaves a file out names the file; a push of a commit made around the hook is denied; a push of another branch says where to run `cw run`; a tag on a commit the remote has, a new branch at that commit and a deletion go through; observe mode prints one line; a missing `cw` and an unreadable store deny; `cw uninstall` removes the hooks and the store. Scenario tests since then, local, Linux: commit gate 27, push gate 17, reporting 16 (15 pass; the offline one is skipped because this machine does not allow a network namespace), platform 8 (3 run; 5 need Windows and are only compiled). Unit tests for `decl`, `store`, `stats`, `gitx`, `evidence` and `gate` pass. |
| M5 | done, not run on a host | `ba569f7` | Local, Linux: the 12 scenarios of `agent-integration` each have a test and pass; they run the hook commands of both hook files with `sh -c` and a notice on standard input. `claude plugin validate .` passes. Nothing was run inside Claude Code or Codex. |
| M6 | done, not run with a model | `ba569f7`, `90aee13` | Local, Linux: `cw roles`, `cw roles check`, `cw roles text`, `cw change status` and `cw change check` exist. The 19 scenarios of `role-definitions` that need no model and the 29 of `change-record` each have a test, written from the specifications by agents that did not read the code; all pass. `cw change check` exits 0 on the two change folders of this repository, after it found 18 requirements that no task named. No role was ever started: the texts were not tried with a model. |
| M7 | done; compared with real records in M9 | `90aee13`, `501ebd8` | Local, Linux: the 16 scenarios of `token-report` each have a test and pass, 14 against records built to the shape the reader expects and 2 as tests of the documents. Three scenario tests failed first and were settled against the specification (see the table above). A record built to the table of the specification gives 655,000 tokens, of which 225,000 (34%) are workflow tokens; no text of a brief is stored. No record that Claude Code or Codex wrote was read. |
| M8 | done, except what needs the owner | `501ebd8` | Local, Linux: `README.md`, eleven documents under `docs/` with an index, and `docs/try-it.md`, whose parts 1 to 9 were run by hand with the output shown. The quick start is executed by a test. Tests of the documents: links and anchors resolve, the index names every document, no benefit is stated while the comparison table holds no numbers. `cw.toml` declares five checks. In a second clone of this branch at `501ebd8`, made in a scratch directory, `cw init` installed the hooks, `cw run` passed all five checks, a commit was allowed, a commit of changed content was denied and named the five checks, and a commit with a failing test was denied and named the check `test`. The owner's clone was not touched: it has no hooks of this `cw` and no records. The two writers of the reference documents compared the program with the design and reported eleven cases with a concrete failure and thirteen differences; all were settled in the code or in the design, and six got a test under `scenarios/messages`. Not done: the release workflow (task 11.2), the hook-cost test (10.1), the git observations as a test (3.5), and everything that needs CI, macOS, Windows, Codex or a model. |
| M9 | done, not run on a host | the commit after `e50e20b` | Local, Linux. **Review:** four research agents read the sources listed in `docs/other-tools.md`; its author did not open them again. Each of 48 findings has an outcome: changed, already so, rejected or deferred. **Skill:** seven examples, each replayed by `TestSkillExamplesAreReplayed` against the `cw` under test; the replay found four places where a draft showed other output than `cw` prints. **Guard:** table tests and four scenario tests through both hook files. **Tokens:** `cw tokens record` and a separate script gave the same totals for 170 real records of Claude Code and for 13 real rollouts of Codex. **Model trial:** five runs of a subagent on Opus 5.5 in scratch repositories, three with the skill and two without, read back from the repositories: no run went around the gate; one run for each cell is not a measurement. `gofmt`, `go vet` and `golangci-lint` for three systems, `govulncheck`, `go test -race -count=1 ./...` in 27 packages, the specification and task lints, and `claude plugin validate .` pass. Not done: a run of the plugin on Claude Code or Codex, the trigger test of the skill, CI. |
| M10 | done, not run on a host | `c5a88ef` to `3eaa40e` | **Review:** eight advisers, 93 findings, each with an outcome under "The second challenge"; nine more defects found while the fixes were tested, the last by a thorough CI run, all fixed; a ninth adviser on the final hook commands, five findings, all resolved, and one more point (H6) rejected against three CI runs; a tenth on the last tests and the closing documents, twenty findings in two passes, all resolved. **CI, run 19, commit `3eaa40e`:** every job passed: the tests on Linux, macOS and Windows, the lint and the vulnerability check for the three builds, the plugin manifests, and `cw run` on this repository. **CI, run 43638791541, started by hand on the same commit:** every job passed with the race detector on the three systems. **Local, Linux:** `go build`, `go vet` and `golangci-lint` for the three systems, `go test -race -count=1 ./...`, `cw change check` and `claude plugin validate .` pass. Nothing was run inside Claude Code or Codex, and nobody tried macOS or Windows by hand. |

### The first run of CI

The branch was pushed on 14 November 2026 and CI ran for the first time, on pull request 219.
The job `Native Go` passed on Linux. Every finding of that run is listed here.

| Finding | System | Outcome |
| --- | --- | --- |
| A test compared the files before and after a refused command while the background maintenance of git removed its lock. | Linux, macOS | Changed: the test repositories start no maintenance. The new test of the harness failed before the change, locally. |
| The guard did not ask about an edit that creates a folder under `.git/cw` or `.git/hooks` when the repository is reached through a link. | macOS, Windows | Changed: the deepest folder that exists is resolved. The test failed before the change on Linux, through a link. |
| The git directory kept forward slashes while the two other directories had the form of the system. | Windows | Changed. Passed in the second run. |
| A store that is a file was read as "no mode", so the gate enforced without reporting the error. | Windows | Changed: the store is looked at before the mode. Passed in the second run. |
| `cw init` failed once with "put process in a job object: Access is denied": the program was ending before it joined its job. The second run showed the same for other git calls. | Windows | Changed: the program starts suspended, joins its job and then continues. Passed in run 4 (commit `dd0ce41`). |
| A child of a program was still running after the run. The cause first named was that the program joined its job after it had started, so under load a child could start first; the suspended start did not end it. The third run showed the real cause: the end of the job did not wait for each process to exit, because Windows takes a process out of the job's count while it is still ending. | Windows | Changed in `5ac846b`: the processes of the job are opened before it is ended, and each is waited for. Passed in run 4 (commit `dd0ce41`). |
| `cw` links the package `net` through `golang.org/x/sys/windows`. | Windows | Changed: the test reads the packages for all three systems and holds that no other package imports a network package. The documents say so. |
| Two tests were wrong for Windows: one wrote a path with backslashes into `cw.toml`, one looked for the text of the hook on the wrong stream of PowerShell. | Windows | Changed. Both passed in the second run. |
| Two runs at once: one was refused the file that holds its temporary index, while Windows was still removing the directory for the other. Found by the second run. | Windows | Changed: the two steps are repeated also when the file is refused, with a short pause. Passed in run 4 (commit `dd0ce41`). |
