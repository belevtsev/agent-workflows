# check-evidence

## Purpose

Record which declared checks passed on which exact content, so that a later
decision rests on a run that `cw` made itself and not on anybody's claim that
the checks pass.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- A git repository `demo` on branch `main` with one commit. Its files:
  - `cw.toml` declaring `unit` with `run = ["sh", "unit.sh"]` and
    `paths = ["src/**", "unit.sh"]`, and `lint` with `run = ["sh", "lint.sh"]`
    and no `paths`.
  - `src/app.txt` holding `ok`. `unit.sh` exits 0 when `src/app.txt` holds
    `ok`, and exits 3 otherwise.
  - `lint.sh` exits 0 unless a file named `BAD` exists at the root.
  - `README.md`, and a `.gitignore` that lists `build/`.
- "Checked state" means `cw run` has just ended with both checks `passed` and
  nothing has changed since.
- A "decisive" run is one that ended `passed`, `failed` or `timed_out`.

## ADDED Requirements

### Requirement: Snapshot

The snapshot of a working tree SHALL be the git tree that `git add -A` followed by `git commit` would record: every tracked file as it is on disk and every untracked file git does not ignore. Computing it SHALL NOT change the index, `HEAD`, any ref or any file in the working tree, and SHALL have no limit on the number or the size of the files.

Source: review findings H1 (untracked files were left out of the commit check), C2 and M5 (`internal/projectctx/capture.go:24-26` caps the source at 30,000 files and 64 MiB); proposal, Sources (snapshot observed equal to the commit's tree).

#### Scenario: An untracked file is part of the snapshot

- **WHEN** `src/new.txt` is created and not staged, and `cw status --json` runs
- **THEN** its `tree` differs from the value before the file existed, and equals `git rev-parse 'HEAD^{tree}'` after `git add -A` and `git commit`

#### Scenario: An ignored file is not

- **WHEN** `build/out.bin` is created and `cw status --json` runs
- **THEN** its `tree` is the same as before the file existed

#### Scenario: The index is left alone

- **WHEN** `src/app.txt` is edited, `src/new.txt` is created, and `cw run` runs
- **THEN** `git status --porcelain` prints the same lines before and after

#### Scenario: A large repository has a snapshot

- **WHEN** the working tree holds 30,001 non-ignored files and a 68 MiB untracked file, and `cw status --json` runs
- **THEN** it prints a `tree` and does not report that the source is too large

### Requirement: Working tree that cannot be recorded

When git cannot record the working tree as it is on disk, `cw run` and `cw status` SHALL exit 1, name the path and the cause, and record nothing. This covers a tracked file marked skip-worktree or assume-unchanged that is absent from disk or differs from the index, a working tree on which `git add -A` fails, and a submodule or nested repository that holds changes it has not committed, holds a file that git is told to overlook, or is not checked out yet holds files.

Source: independent review of this draft, finding 2; proposal, Sources (the tree after `git add -A` equals the tree of `HEAD` in all three cases, and `git add -A` fails with status 128 on a nested repository without a commit); production plan, Independent challenge, finding 3 (the snapshot records a submodule as its commit only); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding G2.

#### Scenario: A local override that git is told to overlook

- **WHEN** `git update-index --skip-worktree README.md` has run, `README.md` is then edited, and `cw run` runs
- **THEN** it exits 1, names `README.md` and the skip-worktree mark, and executes no check

#### Scenario: A sparse checkout

- **WHEN** a sparse checkout keeps `src/` off the disk and `cw status` runs
- **THEN** it exits 1 and says that tracked files are missing from disk because of the sparse checkout

#### Scenario: A nested repository without a commit

- **WHEN** `git init sub` has created an empty repository inside the working tree and `cw run` runs
- **THEN** it exits 1, names `sub/` and git's reason, and executes no check

#### Scenario: A submodule with uncommitted changes

- **WHEN** `vendor/lib` is a submodule, a file inside it is edited and not committed there, and `cw run` runs
- **THEN** it exits 1, names `vendor/lib`, says that it holds uncommitted changes, and executes no check

#### Scenario: A submodule that hides a file

- **WHEN** `vendor/lib` is a submodule, `git update-index --assume-unchanged lib.txt` or `git update-index --skip-worktree lib.txt` has run inside it, `lib.txt` is then edited, and `cw run` or `cw status` runs
- **THEN** it exits 1, names `vendor/lib/lib.txt` and the mark, says to clear the mark there first, and records nothing

#### Scenario: A submodule that is not checked out but holds files

- **WHEN** `git submodule deinit -f vendor/lib` has run, a file is then written into the empty folder `vendor/lib/`, and `cw run` or `cw status` runs
- **THEN** it exits 1, names `vendor/lib`, says that it is a submodule that is not checked out yet its folder holds files, and records nothing

### Requirement: Running checks

`cw run` SHALL execute each declared check that is not satisfied for the snapshot, one at a time in the order of the declaration, from the repository root, with the caller's environment unchanged and with no standard input, and SHALL show each check's output as it is produced. `cw run <id>...` SHALL consider only the named checks, and `--force` SHALL execute a check even when it is satisfied.

Source: review finding M9 (`internal/taskflow/checks.go:299` strips environment variables by substring); proposal, What Changes.

#### Scenario: Both checks run

- **WHEN** `cw run` runs in `demo` for the first time
- **THEN** `unit.sh` and then `lint.sh` are executed once each, the output names `unit` and `lint` as `passed`, and `cw run` exits 0

#### Scenario: A satisfied check is not run again

- **WHEN** `cw run` runs in the checked state
- **THEN** neither script is executed, the output names both checks as satisfied, and `cw run` exits 0

#### Scenario: Only the named check runs

- **WHEN** `cw run lint --force` runs in the checked state
- **THEN** `lint.sh` is executed once and `unit.sh` is not

#### Scenario: The environment reaches the check

- **WHEN** `GIT_CONFIG_KEY_0=user.name` and `TOKENIZERS_PARALLELISM=false` are set and a check prints its environment
- **THEN** the log holds both variables with those values

#### Scenario: An unknown check name is a usage error

- **WHEN** `cw run nosuch` runs
- **THEN** it exits 2, names `nosuch`, and no check is executed

### Requirement: Outcome of a run

Each run SHALL end with exactly one outcome: `passed` when the program exits 0 and the snapshot after it equals the snapshot before it; `source_changed` when the program exits 0 and the snapshot differs; `failed` when the program exits with any other status; `timed_out` when `timeout` passes first; `interrupted` when `cw` is stopped first. `cw run` SHALL exit 5 when a check it executed did not end `passed`, and SHALL then end with one line that says how many did not.

Source: review findings M13 (profile output written into the worktree) and H2; `docs/operations.md` exit status table; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding U3.

#### Scenario: A failing check

- **WHEN** `src/app.txt` holds `broken` and `cw run` runs
- **THEN** `unit` ends as `failed` with exit status 3, `lint` still runs, and `cw run` exits 5

#### Scenario: A check that writes into the source

- **WHEN** a check's program writes `coverage.out` at the root, where git does not ignore it, and exits 0
- **THEN** the run ends as `source_changed`, the output names `coverage.out`, and the check is not satisfied

#### Scenario: A source edit during a run

- **WHEN** `README.md` is edited by another process while `lint.sh` is running, and `lint.sh` exits 0
- **THEN** the run ends as `source_changed` and the output names `README.md`

#### Scenario: A run in which a check did not pass ends with one line

- **WHEN** `src/app.txt` holds `broken` and `cw run` runs
- **THEN** it exits 5 and the last line of its output is ``not verified: 1 of 2 checks did not pass; fix what each log shows, then run `cw run` again``

### Requirement: Leftover processes

When a run ends, `cw` SHALL stop every process the check started that is still running, before it reports the outcome. A process that outlives the program and holds its output open SHALL NOT change an outcome that the program's exit status gave, and `cw` SHALL print a warning that names the cause.

Source: review finding L7 (`internal/taskflow/checks.go:657-659` records such a run as failed with no hint of the cause); `internal/proc/group_other.go` (on a system other than Linux and macOS only the program itself is stopped today).

#### Scenario: A timeout stops the whole process tree

- **WHEN** a check with `timeout = "2s"` runs `sh -c "sleep 600 & sleep 600"`
- **THEN** the run ends as `timed_out` within 10 seconds and no `sleep` process started by the check is still running

#### Scenario: A background process left behind

- **WHEN** a check's program starts `sleep 600` in the background with its output inherited, and exits 0
- **THEN** the run ends as `passed` within 10 seconds, the output warns that a process was left behind, and that `sleep` process is no longer running

#### Scenario: A timeout stops the whole process tree on Windows

- **WHEN** on Windows a check with `timeout = "2s"` runs a program that starts a second program, and neither ends for 600 seconds
- **THEN** the run ends as `timed_out` within 10 seconds and neither program is still running

### Requirement: Interrupted runs

When `cw` receives SIGINT, SIGTERM or SIGHUP during a run, or on Windows Ctrl+C, Ctrl+Break or the closing of its console, it SHALL stop the check and record the run as `interrupted`. A run whose `cw` process ended without recording an outcome SHALL be reported as `interrupted` by every later command. An unfinished or interrupted run SHALL NOT block, delay or change any other run.

Source: review finding H2 (`internal/taskflow/checks.go:560-563`; a killed run stays "running" and refuses a rerun); `go doc os/signal`, section Windows, Go 1.27.4 (which of these events a program can catch on Windows).

#### Scenario: `cw` is killed during a run

- **WHEN** `cw.toml` also declares `slow` with `run = ["sleep", "60"]`, `cw run slow` is executing it, the `cw` process receives SIGKILL, and `cw run slow` runs again
- **THEN** the second command starts a new run at once, and `cw status` shows no run of `slow` as running

#### Scenario: The terminal closes

- **WHEN** `cw.toml` also declares `slow` with `run = ["sleep", "60"]`, `cw run slow` is executing it, and the `cw` process receives SIGHUP
- **THEN** `cw` exits within 10 seconds, the run is recorded as `interrupted`, and the `sleep` process is no longer running

#### Scenario: Ctrl+Break on Windows

- **WHEN** on Windows `cw.toml` also declares `slow`, whose program does not end for 60 seconds, `cw run slow` is executing it, and the `cw` process receives Ctrl+Break
- **THEN** `cw` exits within 10 seconds, the run is recorded as `interrupted`, and the program of `slow` is no longer running

### Requirement: What a run records

For each run `cw` SHALL record the check's `id`, `run` and `paths`, the tree of the snapshot it ran on, the outcome, the program's exit status, the start and end times, and the path of a log that holds the check's output. A run SHALL be recorded for the snapshot taken immediately before the program started.

Source: proposal, Terms; `docs/evidence.md` (a run is bound to a source digest today).

#### Scenario: The record of a failed run

- **WHEN** `src/app.txt` holds `broken`, `cw run unit` ends, and `cw status --json` runs
- **THEN** the entry for `unit` holds `outcome` `failed`, `exit_status` 3, a `tree` equal to the top-level `tree`, and a `log` path whose file holds the output of `unit.sh`

### Requirement: Log limit

`cw` SHALL keep at most 10 MiB of a run's output, the first 1 MiB and the last 9 MiB, and SHALL mark in the log where output was dropped. The limit SHALL NOT change the outcome of the run or whether the run satisfies a check.

Source: review finding M14 (`internal/taskflow/reuse.go:233` refuses reuse whenever the log was cut at 128 KiB).

#### Scenario: A very long log

- **WHEN** a check prints 50 MiB and exits 0, and `cw run` runs twice
- **THEN** the log file is at most 10 MiB plus the marker, the first run ends `passed`, and the second `cw run` executes nothing

### Requirement: Same checked content

For one check, two trees SHALL hold the same checked content exactly when no path that differs between them matches the check's `paths`; for a check without `paths`, only identical trees do. A path differs when it exists in only one of the trees, or its content or its mode is different. `cw` SHALL NOT detect renames or copies: a moved file differs at its old path and at its new one. Every submodule SHALL count in the checked content of every check with `paths`.

Source: proposal, Terms; independent review of this draft, finding 6; proposal, Sources (`git diff-tree -r --name-only` lists a moved file at both paths); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding G3.

#### Scenario: A file moved out of `paths`

- **WHEN** `git mv src/app.txt docs/app.txt` runs in the checked state and `cw status` runs
- **THEN** it reports `unit` as not run

#### Scenario: A file made executable

- **WHEN** `chmod +x unit.sh` runs in the checked state, in a repository where git records file modes, and `cw status` runs
- **THEN** it reports `unit` as not run

#### Scenario: A submodule counts for every check with `paths`

- **WHEN** `vendor/lib` is a submodule, `cw run` exits 0, a new commit is then made inside `vendor/lib`, and `cw status` runs
- **THEN** it exits 5 and reports `unit` as not run on this content, although the `paths` of `unit` do not match `vendor/lib`

### Requirement: Satisfied check

A check declared in a tree SHALL be satisfied for that tree exactly when the most recent decisive run of a check recorded with the same `id`, `run` and `paths`, on the same checked content, ended `passed`. The most recent run SHALL be the one whose end `cw` recorded last, in the order of recording and not by the clock; when that order cannot be told, a run that did not pass SHALL count as the later.

Source: review finding M8 (`internal/taskflow/executed.go:51-57`; a pass after a failed run earned no credit); proposal, Terms; independent review of this draft, finding 10; production plan, Independent challenge, finding 2 (a clock that was set back must not let an earlier pass outrank a later failure).

#### Scenario: A run on the same tree satisfies

- **WHEN** `cw status` runs in the checked state
- **THEN** it reports `unit` and `lint` as satisfied

#### Scenario: A change outside `paths` keeps a check satisfied

- **WHEN** `README.md` is edited in the checked state and `cw status` runs
- **THEN** it reports `unit` as satisfied and `lint` as not run

#### Scenario: A change inside `paths` does not

- **WHEN** `src/app.txt` is rewritten with the same word and a trailing space in the checked state, and `cw status` runs
- **THEN** it reports `unit` as not run

#### Scenario: A changed command needs a new run

- **WHEN** the `run` of `unit` in `cw.toml` becomes `["sh", "unit.sh", "-v"]` in the checked state and `cw status` runs
- **THEN** it reports `unit` as not run

#### Scenario: A later pass replaces an earlier failure on the same content

- **WHEN** a check `env` with `run = ["test", "-e", "/tmp/demo-flag"]` ends `failed` because `/tmp/demo-flag` does not exist, that file is then created outside the repository, and `cw run env` runs again
- **THEN** the program is executed again, the run ends `passed`, and `cw status` reports `env` as satisfied

#### Scenario: A later failure replaces an earlier pass

- **WHEN** the check `env` of the previous scenario has ended `passed`, `/tmp/demo-flag` is deleted, and `cw run env --force` ends `failed`
- **THEN** `cw status` reports `env` as failed

#### Scenario: Two runs that overlap

- **WHEN** in two linked worktrees with the same content, a run of `env` starts first and ends `failed`, and a run of `env` that started after it ended `passed` before that
- **THEN** `cw status` reports `env` as failed, because the failed run ended last

#### Scenario: The clock does not decide the order

- **WHEN** the check `env` has ended `passed`, `/tmp/demo-flag` is deleted, `cw run env --force` ends `failed`, and the `ended_at` in the record of that failed run is then changed to one hour earlier
- **THEN** `cw status` reports `env` as failed

#### Scenario: Content checked earlier is recognised again

- **WHEN** `src/app.txt` is changed to `broken` in the checked state and then changed back to `ok`, and `cw status` runs
- **THEN** it reports `unit` and `lint` as satisfied

### Requirement: Verified tree

A tree SHALL be verified exactly when it holds a valid declaration and every check in that declaration is satisfied for the tree. A tree with no declaration, or with an invalid one, SHALL NOT be verified. A tree whose valid declaration holds no checks SHALL be verified.

Source: proposal, Terms and Open Question 3; review finding C2.

#### Scenario: Every check is satisfied

- **WHEN** `cw status --json` runs in the checked state
- **THEN** `verified` is `true`

#### Scenario: One check is not satisfied

- **WHEN** `README.md` is edited in the checked state and `cw status --json` runs
- **THEN** `verified` is `false`

### Requirement: Old runs

A run that ended more than 30 days ago SHALL NOT satisfy a check, and `cw run` SHALL remove its log. A run whose recorded end is later than the present time SHALL NOT satisfy a check either. `cw run` SHALL remove the records of runs and of hook decisions that are more than 365 days old.

Source: review finding M4 (the store grows without bound today); proposal, Open Question 5; `gate-reporting`, Statistics (which needs records older than 30 days); production plan, Independent challenge, finding 2 (a pass dated in the future would never age out).

#### Scenario: A run ages out

- **WHEN** the checked state is 31 days old and `cw status` runs
- **THEN** it reports `unit` and `lint` as not run

#### Scenario: A run dated in the future

- **WHEN** the `ended_at` in the record of the passing `lint` run is changed to one day ahead in the checked state, and `cw status` runs
- **THEN** it reports `lint` as not run and warns that the run is dated in the future

### Requirement: Base revision for continuous integration

`cw run --base <rev>` SHALL, for that command only, count each check that the tree of `<rev>` declares with the same `id`, `run` and `paths` as passed on that tree, earlier than every recorded run. It SHALL then execute the checks that are not satisfied, and SHALL NOT record a run for a check it did not execute.

Source: proposal, Non-goals (evidence is not shared with CI) and Known limits (`paths` is a promise).

#### Scenario: A documentation change in a pull request

- **WHEN** a fresh clone with no records has `HEAD` differing from `origin/main` only in `README.md`, and `cw run --base origin/main` runs
- **THEN** `unit.sh` is not executed, `lint.sh` is executed once, and `cw run` exits 0

#### Scenario: A check new in the change always runs

- **WHEN** `HEAD` adds a check `build` with `paths = ["src/**"]` that the tree of `origin/main` does not declare, changes no file under `src/`, and `cw run --base origin/main` runs
- **THEN** the program of `build` is executed

### Requirement: Record store

`cw` SHALL keep runs, logs and decisions under `cw/` in the repository's common git directory, shared by all linked worktrees. Apart from `cw.toml` and the hook files that `cw init`, `cw add` and `cw uninstall` write, `cw` SHALL NOT create or change any file outside the git directory.

Source: review findings H9 and M4 (the task store is under `~/.local/state/agent-workflows` with a full copy per revision, `internal/taskflow/storage.go:42`).

#### Scenario: The working tree and the home directory stay clean

- **WHEN** `cw run` ends in a clean checkout of `demo` with `HOME` set to an empty directory
- **THEN** `git status --porcelain` prints nothing and `HOME` is still empty

#### Scenario: A linked worktree sees the same records

- **WHEN** `git worktree add ../wt` creates a worktree at the same commit in the checked state, and `cw status` runs in `../wt`
- **THEN** it reports `unit` and `lint` as satisfied

### Requirement: Damaged and concurrent records

A record that `cw` cannot read SHALL NOT count as a pass, and `cw` SHALL warn with the name of the file. Its check SHALL NOT be satisfied for that checked content until a later run of it ends decisively; other checks and other content SHALL be judged as before. Commands that run at the same time in one repository or its worktrees SHALL NOT lose or damage the record of a finished run. A finished record SHALL be on disk before it replaces the unfinished one.

Source: review findings M4 and L8 (`internal/taskflow/storage.go:278-386`; every load re-validates the whole chain, so one bad revision makes a task unreadable); production plan, Independent challenge, finding 4 (a damaged record of a failure must not let an earlier pass decide).

#### Scenario: A truncated record

- **WHEN** the record of the passing `lint` run is cut to half its length in the checked state and `cw status` runs
- **THEN** it warns with the path of that record, reports `lint` as not run and `unit` as satisfied

#### Scenario: A damaged record hides no failure

- **WHEN** the check `env` has ended `passed` and then, with `--force`, `failed` on the same content, the record of the failed run is cut to half its length, and `cw status` runs
- **THEN** it reports `env` as not run; after `/tmp/demo-flag` is created again and `cw run env` ends `passed`, `cw status` reports `env` as satisfied

#### Scenario: Two runs at once

- **WHEN** `cw run unit --force` and `cw run lint --force` run at the same time in two linked worktrees at the same commit
- **THEN** both end `passed` and `cw status` reports both checks as satisfied

### Requirement: No network

`cw` SHALL NOT open a network connection and SHALL NOT send any content, path, name or statistic to any other machine. This does not limit what a check's own program does.

Source: review finding H5 (`internal/consult/provider.go:225` sends the repository path, branch and HEAD to adviser APIs).

#### Scenario: Everything works offline

- **WHEN** `cw init`, `cw add`, `cw run`, `cw status`, `cw stats` and a gated `git commit` run in a network namespace that has no interface except loopback
- **THEN** each behaves as it does with networking available
