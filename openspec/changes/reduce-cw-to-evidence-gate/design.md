# Design: reduce-cw-to-evidence-gate

Status: written on 14 November 2026 for an implementation that runs without
the owner. It turns the six specifications into decisions an implementer does
not have to make again. Where it chooses something the specifications leave
open, it says so under "Decisions taken without the owner".

## Context

- **Problem.** `cw` allows unverified commits (C2, H1), blocks finished work
  (H2, H9) and cannot be installed (C1). The specifications replace it with a
  gate that is bound to git trees.
- **Who it is for.** Any developer or agent that uses git in a repository of
  any language, on Linux, macOS or Windows.
- **Outcome.** Five commands take a new repository to its first gated commit,
  and nothing is committed or pushed without a passing run of every declared
  check on exactly that content.
- **Base.** Branch `feat/evidence-gate-and-roles`, cut from `origin/main` at
  `3cbfb0d`, two commits after `ce9b122`, the commit the review and the
  keep-and-cut list read. Today's workflow is kept as the local branch
  `workflow-v3` at the same commit.
- **Environment of the build.** Linux, Go 1.27.4, git 2.53.0. No macOS and no
  Windows machine, and no CI run, because nothing is pushed.

## Decisions taken without the owner

| # | Decision | Why | How to undo |
| --- | --- | --- | --- |
| 1 | The work is on a branch in a worktree, not on `main`. | The task list says `main`; the owner's daily `cw` and plugin load from `main`, and nobody is there to approve. | Merge the branch when satisfied. |
| 2 | Task 1.1: the module path stays `github.com/belevtsev/agent-workflows/v3`. No tag is made. | It is the recommended answer, and a tag is the owner's. | Change `go.mod` and the imports. |
| 3 | Open Questions 9 to 11 take their recommendations. | The owner asked for the work to go on alone. | Each is one function or one file. |
| 4 | The command line uses the standard `flag` package, not cobra. | cobra's flag package links Go's `net` package. Without it a test can show that `cw` links no network code at all (`check-evidence`: No network). | Put cobra back; the commands do not change. |
| 5 | Nothing is deleted. Files leave by `git mv` into `_quarantine/`. | The owner's rule. | Move them back. |
| 6 | Commits are local, authored with the identity earlier local commits used. Nothing is pushed, tagged or released, and no repository setting changes. | Publishing is the owner's. | `git branch -D` removes all of it. |

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `cmd/cw` | Starts the program: signals and the exit status | `internal/cli` | no | |
| `internal/cli` | The commands, their flags and their messages | `internal/gate`, `internal/stats`, `internal/runner`, `internal/evidence` | no | |
| `internal/gate` | The hook files and the three hook decisions | `internal/evidence` | no | |
| `internal/stats` | Counts from the records | `internal/store` | yes | `internal/cli/metrics.go` on `workflow-v3`: it reads the task store, which leaves |
| `internal/runner` | Executes one check and captures its log | `internal/store`, `internal/proc`, `internal/gitx` | yes | `internal/taskflow/checks.go` on `workflow-v3`: bound to a task and a plan; `internal/proc` is reused |
| `internal/evidence` | Decides satisfied and verified from records | `internal/store`, `internal/decl`, `internal/gitx` | yes | `internal/taskflow/find.go` on `workflow-v3`: it decides from task records and skips what it cannot digest (C2) |
| `internal/store` | Runs, logs, decisions, the mode, and clean-up | `internal/fslock` | yes | `internal/taskflow/storage.go` on `workflow-v3`: a chain of revisions under the home directory (H9, M4, L8); `internal/fslock` is reused |
| `internal/decl` | `cw.toml`: parse, validate, patterns, append | none | yes | `internal/modelpolicy` on `workflow-v3`: a typo in a key is silent there (L10) |
| `internal/gitx` | Every call to git | `internal/proc` | yes | `internal/projectctx/capture.go` on `workflow-v3`: it hashes every file itself, with a ceiling (C2, M5) |
| `internal/proc` | A program in its own process group | none | no | |
| `internal/fslock` | A lock a dead process cannot leave behind | none | no | |

`internal/cli` is the only package that imports all of them, and no package
imports `internal/cli`. The Go modules that remain are `go-toml/v2` and
`golang.org/x/sys`.

## Rules

| Rule | Checked by |
| --- | --- |
| No package imports `internal/cli` except `cmd/cw` | review |
| `cw` links no network package | review |
| Only `internal/gitx` starts git | review |
| Nothing outside the git directory is written, apart from `cw.toml` and the hook files | review |

"review" stands for a Go test in each case; this repository has no `cw.toml`
yet, so no check id can be named. Task 6.3 of the second change replaces the
word when it has.

## Git

All git calls go through `internal/gitx`, with the caller's environment, from
the repository root.

- **Snapshot.** Copy the index git would use (`GIT_INDEX_FILE` when set, else
  the path `git rev-parse --git-path index` gives) to `cw/tmp/` in the common
  git directory; when there is no index, start empty. With `GIT_INDEX_FILE`
  set to the copy, run `git add -A` and `git write-tree`, then remove the
  copy. The copy keeps git's file statistics, so only changed files are
  hashed.
- **A working tree that cannot be recorded.** Before the snapshot,
  `git ls-files -v` lists the marks. For a path marked skip-worktree (`S`) or
  assume-unchanged (a lower-case tag) that is absent from disk, or whose
  `git hash-object` differs from the index entry, stop with exit 1 and name
  the path and the mark. When `core.sparseCheckout` is true and the path is
  absent, say that the sparse checkout keeps it off the disk. When
  `git add -A` fails, print the path git names and git's own reason.
- **The tree of a commit.** Inside `pre-commit` and `pre-merge-commit`,
  `git write-tree` with the environment git gave the hook.
- **Paths that differ.** `git diff-tree -r --no-renames --name-only -z A B`.
- **The declaration of a tree.** `git ls-tree T -- cw.toml`, then
  `git cat-file blob`. No entry means no declaration; an entry that is not a
  regular file is an invalid declaration.
- **The entries of a tree.** `git ls-tree -r -z T`, once per tree. A submodule
  and a symbolic link are one entry each.

## Declaration

- Parsed from the bytes of `cw.toml` in the tree that is judged, never from a
  second place. `cw run` and `cw status` read it from the snapshot, so a
  `cw.toml` that git ignores is no declaration.
- `go-toml` decodes with unknown keys refused; its error gives the line. The
  fields of a check are decoded loosely and validated by `cw`, so each broken
  rule has its own sentence. The line of a check is the line of its
  `[[check]]` header, found by scanning the file.
- A pattern is split at `/`. `**` is a whole segment that matches any number
  of segments, also none. `*` and `?` match inside one segment. Nothing else
  is special. An empty pattern and one that begins with `/` are refused.
- `cw add` appends a block of text, after a newline when the file does not end
  with one, and refuses when the result does not parse. Strings are written as
  TOML basic strings.

## Records

Everything is under `cw/` in the common git directory.

```
cw/mode                      "enforce" or "observe"
cw/runs/<check id>/<run id>.json
cw/runs/<check id>/<run id>.lock   held while the run executes
cw/logs/<run id>.log
cw/decisions/<yyyy-mm>.jsonl       one line per hook decision
cw/tmp/                            index copies
```

- **Run id.** The start time in UTC and eight random hex digits:
  `20261114T153000Z-1a2b3c4d`.
- **Run record.** Written when the program starts, without an outcome, and
  replaced by rename when it ends. Fields: `run_id`, `id`, `run`, `paths`,
  `tree`, `content_key`, `outcome`, `exit_status` (absent when the program did
  not exit on its own), `started_at`, `ended_at`, `log`.
- **Content key.** For a check without `paths`, the tree id. With `paths`, the
  SHA-256 of the mode, object id and path of every entry of the tree that
  matches a pattern. Two trees hold the same checked content for a check
  exactly when their keys are equal, so no hook compares trees pairwise.
- **A run that died.** A record without an outcome whose lock can be taken at
  once belongs to a dead `cw`; it is reported as `interrupted`. No command
  waits for a lock of another run.
- **Satisfied.** Among the runs of a check with the same `id`, `run`, `paths`
  and content key that ended `passed`, `failed` or `timed_out` in the last 30
  days, the one that ended last decides; of two that ended at the same
  instant, the one that did not pass.
- **A record that cannot be read** is skipped with a warning that names the
  file.
- **Clean-up.** `cw run` removes logs of runs that ended more than 30 days ago
  and records more than 365 days old, under `cw/gc.lock` taken without
  waiting; a busy lock skips the clean-up.
- **Decision line.** `time`, `hook`, `mode`, `tree`, `outcome` (`allowed` or
  `denied`), `ref` for a push, `checks` (the state of each check that was not
  satisfied) or `cause` (`no_declaration`, `invalid_declaration`, `tree`,
  `store`, `internal`).

## Running a check

For each selected check, in the order of the declaration:

1. Take the snapshot and its content key. When the check is satisfied and
   `--force` is not given, say so and go on.
2. Write the run record and take its lock.
3. Start the program through `internal/proc`: from the repository root, with
   the caller's environment, no input, its own process group, and the check's
   timeout. Output goes to the terminal as it arrives and to the log, which
   keeps the first 1 MiB and the last 9 MiB with a marker between them.
4. Take the snapshot again. The outcome is `passed` for exit 0 and an equal
   snapshot; `source_changed` for exit 0 and a different one, with up to 20
   differing paths; `failed` for another exit status or a program that could
   not be started, with the program named; `timed_out`; or `interrupted`.
5. Replace the record and release the lock.

`cw run` exits 5 when a check it executed did not end `passed`, 2 for an
unknown check name, and 1 when the declaration or the working tree stops it.
SIGINT, SIGTERM and SIGHUP, and on Windows Ctrl+C, Ctrl+Break and the closing
of the console, cancel the run; the process group is stopped before the
record is written.

## Hooks

- **The hook file** is a POSIX shell script with a marker line that shows `cw`
  wrote it. It runs `cw hook <name> "$@"`. When `cw` is not on `PATH` it
  prints that `cw` was not found and where the install instruction is, and
  exits 3. Git for Windows runs the same file in its own shell.
- **`cw init`** creates `cw.toml` when it is missing, installs the three hooks
  in `hooks/` of the common git directory, and writes the mode. A hook file
  without the marker is left alone, and `core.hooksPath` stops every write;
  both end with exit 4 and the line to add.
- **`pre-commit` and `pre-merge-commit`.** Write the tree of the index, read
  its declaration, list its entries once, compute the key of each check and
  read that check's records. Verified means allow. The working tree's snapshot
  is taken only for a denial, to list up to 20 paths that differ.
- **`pre-push`.** One decision per line of input. A deletion is allowed. A
  branch update is allowed when the remote's commit already contains the new
  tip; any other ref when a remote-tracking branch of that remote contains its
  commit. Otherwise the tree of the tip commit must be verified. One denial
  denies the push.
- **Notice.** When the new tree lacks a check the tree of `HEAD` declares, or
  declares it with another `run`, `paths` or `timeout`, the hook lists it,
  whatever it decides.
- **Fail closed.** Any cause that prevents a decision is a denial with exit 3
  that names the cause. In observe mode the hook records what it would have
  decided, prints one line and exits 0.
- **A denial** names each check with its state and log, ends with `cw run`,
  says where to run it, and names no way around the gate.

## Platform

- **Windows locks:** `LockFileEx`, released by the system when the handle
  closes or the process dies.
- **Windows process tree:** a job object with kill-on-close, to which the
  program is assigned as soon as it has started. A process the program starts
  in that instant can escape; this limit is stated in the documents.
- **Windows programs:** found by `PATHEXT`, as the system does. For a `.bat`
  or `.cmd` file, an argument that holds anything but letters, digits and
  `_ . / : = + , @ \ -` makes `cw run` refuse the check and name every such
  argument. This is the recommended answer to Open Question 9, in its safe
  form: refuse, never guess a quoting.
- **Release:** archives for the three systems on amd64 and arm64, and
  `SHA256SUMS`.

## Validation

| What | How | Where it runs | What it shows, and what it does not |
| --- | --- | --- | --- |
| The 136 scenarios | Tests that build `cw` and drive it and git in temporary repositories, written from the specifications by another agent than the implementer | Linux, locally | The behaviour on Linux. Scenarios that name Windows are written and skipped there. |
| No network | A test that lists the packages `cw` links and finds no `net` | Linux | That no network code is linked; stronger than the scenario asks. |
| Race conditions | `go test -race ./...` | Linux | Data races the tests reach. |
| Other systems | `GOOS=darwin` and `GOOS=windows` builds and `go vet` | Linux | That the code compiles. Nothing about behaviour. |
| Hook cost | A test that measures a hook decision on this repository and on a generated one of 30,000 files and 68 MiB | Linux | The two limits of Success measure 2 on this machine. |
| CI | The workflow files are written | Not run | Nothing, until the branch is pushed. |
| Lint | `golangci-lint run` | Linux | The repository's own lint rules. |

## Failure behaviour

| Condition | What `cw` does |
| --- | --- |
| The store directory cannot be read or written | A command exits 1 and names the directory; a hook denies and names it. |
| A record is damaged | It is skipped with a warning; other records count. |
| `cw` dies during a run | The next command reports that run as `interrupted`; nothing waits. |
| Two `cw run` at once | Each writes its own record; the later end decides. |
| The index holds a conflict | `git write-tree` fails; the hook denies and prints git's reason. |
| `cw.toml` is missing or invalid in the judged tree | A command exits 1; a hook denies. |
| A check's program does not exist | The run ends `failed` and names the program. |
| The disk is full while a record is written | The rename never happens, so the unfinished record reads as `interrupted`. |

## Rollback

Everything is on one local branch. Deleting the branch and the worktree
restores the state before this work. `main` and the owner's installed `cw` are
untouched.

## Independent challenge

See the section of the same name in
[the production plan](../../production-plan.md): one reviewer read this design
and the role design; its findings and what was done about each are listed
there.
