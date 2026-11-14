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
| 4 | The command line uses the standard `flag` package, not cobra. | cobra's flag package links Go's `net` package. Without it a test can show that `cw` links no network code at all on Linux and macOS, and none of its own on Windows (`check-evidence`: No network). | Put cobra back; the commands do not change. |
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
| `internal/proc` | A program in its own process group; on Windows in a job object | none | no | The Windows part is new: `group_windows.go` |
| `internal/fslock` | A lock a dead process cannot leave behind | none | no | The Windows part is new: `fslock_windows.go`, on `LockFileEx` |

`internal/cli` is the only package that imports all of them, and no package
imports `internal/cli`. The Go modules that remain are `go-toml/v2` and
`golang.org/x/sys`.

## Rules

| Rule | Checked by |
| --- | --- |
| No package imports `internal/cli` except `cmd/cw` | test |
| `cw` links no network package (on Windows: none but through `golang.org/x/sys/windows`) | test |
| A program is started only by `internal/proc`, which only `internal/gitx` (git) and `internal/runner` (a declared check) import | test |
| Nothing outside the git directory is written, apart from `cw.toml` and the hook files | review, test |

`test` is the check of this repository's `cw.toml` that runs every Go test.
The first three rules are tests of `internal/repocheck`. The fourth is tested
where a scenario says that a command writes nothing, and reviewed elsewhere.

## Git

All git calls go through `internal/gitx`, with the caller's environment, from
the repository root.

- **Snapshot.** Copy the index git would use (`GIT_INDEX_FILE` when set, else
  the path `git rev-parse --git-path index` gives) to `cw/tmp/` in the common
  git directory, and give the copy the modification time of the original;
  when there is no index, start empty. With `GIT_INDEX_FILE` set to the copy,
  run `git add -A` and `git write-tree`, then remove the copy. The copy keeps
  git's file statistics, so only changed files are hashed. The modification
  time matters: git re-reads a file whose time is not older than the index,
  and a copy with a new time would make git trust a file that was edited in
  the same second it was staged.
- **The tree of the index.** The same copy, with `git write-tree` alone. No
  command and no hook runs `git write-tree` on the index itself, because that
  rewrites the index file.
- **A working tree that cannot be recorded.** Before the snapshot,
  `git ls-files -v` lists the marks. For a path marked skip-worktree (`S`) or
  assume-unchanged (a lower-case tag) that is absent from disk, or whose
  `git hash-object` differs from the index entry, stop with exit 1 and name
  the path and the mark. When `core.sparseCheckout` is true and the path is
  absent, say that the sparse checkout keeps it off the disk. When
  `git add -A` fails, print the path git names and git's own reason. The
  marks are read from the copy after `git add -A`, which leaves marked
  entries as they were, so one listing serves both this and the next rule.
- **A submodule or nested repository.** The snapshot records it as one
  commit. For each entry with mode 160000 in the copy, `git status
  --porcelain` runs inside it; any output stops the command with exit 1,
  naming the path and saying that it holds uncommitted changes. Otherwise a
  check would run on content that no tree records.
- **The tree of a commit.** Inside `pre-commit` and `pre-merge-commit`, the
  tree of the index, as above, with the environment git gave the hook.
- **The tree of a pushed ref.** `git rev-parse <id>^{commit}` peels an
  annotated tag; its tree is judged. A ref that names a tree is judged by
  that tree. A ref that names anything else is denied, cause `tree`.
- **Paths that differ.** `git diff-tree -r --no-renames --name-status -z A B`, which also gives the kind of each change: added, changed or removed.
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
  is special. A pattern with an empty segment is refused: an empty pattern,
  one that begins or ends with `/`, and one that holds `//`.
- `cw add` appends a block of text, after a newline when the file does not end
  with one, and refuses when the result does not parse. Strings are written as
  TOML basic strings.

## Records

Everything is under `cw/` in the common git directory.

```
cw/mode                                  "enforce" or "observe"
cw/runs/<check id>/<content key>/<run id>.json
cw/runs/<check id>/<content key>/<run id>.lock   held while the run executes
cw/runs/<check id>/<content key>/seq.lock        held while an end is recorded
cw/logs/<run id>.log
cw/decisions/<yyyy-mm>.jsonl             one line per hook decision
cw/tmp/                                  index copies
```

- **The mode.** Observe only when the file holds exactly `observe`, with or
  without a final newline. Anything else, and no file, is enforce.
- **One directory per checked content.** A hook reads only the directory of
  the content it judges, so its cost does not grow with the history.

- **Run id.** The start time in UTC and eight random hex digits:
  `20261114T153000Z-1a2b3c4d`.
- **Run record.** The run's lock is taken first; then the record is written,
  without an outcome. When the run ends, the finished record is written to a
  temporary file, synced to disk, renamed over the unfinished one, and the
  directory is synced. Fields: `run_id`, `id`, `run`, `paths`, `tree`,
  `content_key`, `seq`, `outcome`, `exit_status` (absent when the program did
  not exit on its own), `started_at`, `ended_at`, `log` (relative to `cw/`),
  and for `source_changed` up to 20 `differ` paths.
- **Sequence number.** Under `seq.lock` of the directory, a finished record
  gets `seq`, one more than the highest in that directory. It is the order in
  which ends were recorded, and it does not depend on the clock.
- **Content key.** For a check without `paths`, the tree id. With `paths`, the
  SHA-256 of the raw entries of `git ls-tree -r -z` (mode, type, object id,
  path, each ended by a NUL byte) whose path matches a pattern. Two trees
  hold the same checked content for a check exactly when their keys are
  equal, so no hook compares trees pairwise.
- **A run that died.** A record without an outcome whose lock can be taken at
  once, shared, belongs to a dead `cw`; it is reported as `interrupted`. No
  command waits for a lock of another run.
- **Satisfied.** Among the records in the directory of the check and the
  content key that hold the same `run` and `paths` and ended `passed`,
  `failed` or `timed_out`, the one with the highest `seq` is the most recent;
  of two with the same `seq`, the one that did not pass. It decides only when
  it ended neither more than 30 days ago nor later than now. An older record
  never decides in its place, so a pass cannot return because a later
  failure aged out or carries a wrong date.
- **A record that cannot be read** makes its check not satisfied for that
  content, with a warning that names the file. The next run of the check on
  that content that ends decisively renames the file to `<name>.damaged`,
  after which it is ignored. A record dated in the future is reported in the
  same way and never renamed.
- **`--base`.** `cw run --base <rev>` reads the declaration of the tree of
  `<rev>` and computes that tree's content key for each of its checks. A
  check of the snapshot that the base declares with the same `id`, `run` and
  `paths`, with the same content key, and with no decisive record of its own
  in that directory, counts as satisfied for this command. Nothing is
  written for it. The base implies nothing for a check one of whose records
  cannot be read or is dated in the future, and `paths` are the same only in
  the same order.
- **Clean-up.** `cw run` removes the logs that were last written more than
  30 days ago, the records and the decision files last written more than 365
  days ago, and its copies of the index older than a day, under `cw/gc.lock`
  taken without waiting; a busy lock skips the clean-up. The age is the time
  the file was last written, which for a log and a record is the end of the
  run.
- **Decision line.** `time`, `hook`, `mode`, `tree`, `outcome` (`allowed` or
  `denied`), `ref` for a push, `checks` (the state of each check that was not
  satisfied: `failed`, `not_run` or `timed_out`) or `cause` (`no_declaration`,
  `invalid_declaration`, `tree`, `store`, `internal`).
- **Index copies.** A copy lives in `cw/tmp/` only while git works on it. The
  command that made it removes it and then removes `cw/tmp/` and `cw/` when
  they are left empty, so a command that only reads leaves no directory
  behind. A writer that finds its directory gone creates it again.
- **Statistics.** `cw stats` reads every run record and the decision lines of
  the period. `--since` takes a number and a unit: `90m`, `36h`, `7d`, `2w`;
  the default is 30 days. `cw stats --json` prints one object:

  ```json
  {
    "since": "2026-11-07T10:00:00Z",
    "until": "2026-11-14T10:00:00Z",
    "runs": { "unit": { "passed": 6, "failed": 2 }, "lint": { "passed": 7 } },
    "pass_after_failure": { "unit": 1 },
    "decisions": [
      { "hook": "pre-commit", "mode": "enforce", "outcome": "allowed", "count": 5 },
      { "hook": "pre-commit", "mode": "enforce", "outcome": "denied", "count": 3,
        "reasons": { "failed": 2, "not_run": 1 },
        "checks": { "failed": { "unit": 2 }, "not_run": { "unit": 1, "lint": 1 } } },
      { "hook": "pre-commit", "mode": "observe", "outcome": "denied", "count": 1,
        "reasons": { "failed": 1 }, "checks": { "failed": { "unit": 1 } } }
    ],
    "unreadable": 0
  }
  ```

  `decisions` holds one entry for each hook, mode and outcome that occurred.
  `reasons` counts a denial once under each state its unsatisfied checks had,
  or under its cause; `checks` counts per state how often each check was not
  satisfied. `pass_after_failure` counts, per check, the passing runs whose
  previous decisive run on the same content key, with the same `run` and
  `paths`, did not pass. A run belongs to the period when it ended in it, or, when it never
  recorded an end, when it started in it.

## Running a check

For each selected check, in the order of the declaration:

1. Take the snapshot and its content key. When the check is satisfied and
   `--force` is not given, say so and go on.
2. Take the run's lock and write its record.
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
  without the marker is left alone, and `core.hooksPath` stops every write.
  A hook that is left alone and does not call `cw hook <name>` yet ends
  `cw init` with exit 4 and the line to add; one that calls it already
  needs nothing.
- **`pre-commit` and `pre-merge-commit`.** Write the tree of the index, read
  its declaration, list its entries once, compute the key of each check and
  read that check's records. Verified means allow. The working tree's snapshot
  is taken only for a denial, to list up to 20 paths that differ.
- **`pre-push`.** One decision per line of input. A deletion is allowed. The
  id git passes is peeled to a commit first. A branch update is allowed when
  the remote's commit already contains the new tip; any other ref when a
  remote-tracking branch of that remote contains its commit. Otherwise the
  tree of the tip commit must be verified. One denial denies the push.
- **Gate state.** The hooks are `installed` when each of the three files git
  would run (in `core.hooksPath` when that is set, else in `hooks/` of the
  common git directory) exists, can be executed (not judged on Windows) and
  holds `cw hook <name>`. `cw status` and `cw context` report anything else
  and name `cw init`: it installs the hooks, or, where `core.hooksPath` is
  set, prints the line to add to each.
- **Notice.** When the new tree lacks a check the tree of `HEAD` declares, or
  declares it with another `run`, `paths` or `timeout`, the hook lists it,
  whatever it decides.
- **Fail closed.** Any cause that prevents a decision is a denial with exit 3
  that names the cause. In observe mode the hook records what it would have
  decided, prints one line and exits 0.
- **A denial** names each check with its state and log, ends with `cw run`,
  says where to run it, and names no way around the gate.

## Plugin

- **Session start.** The hook command is shell text: when `cw` is on `PATH`
  it runs `cw context`; when it is not and the root of the repository holds
  `cw.toml`, it prints one line that says so and names the install
  instruction. The Codex command for Windows is `cw context` alone, so that
  line is missing there. The text an agent reads lives in `cw`, not in the
  hook file.
- **Before a tool call.** The hook runs `cw guard --host <host>`, which reads
  the host's notice from standard input: `tool_name`, `cwd` and `tool_input`
  (`command` for the shell tool, `file_path` for an editing tool, the patch
  text for the `apply_patch` of Codex). In a repository whose root holds
  `cw.toml` it answers in two cases: a git call in the command skips the
  hooks, or the edited file is that `cw.toml`, a hook file or a record of
  `cw`. It reads the command as shell words with `internal/shellwords`, a
  splitter that runs nothing: a call skips the hooks when it holds
  `--no-verify`, `-n` of `git commit` or a setting of `core.hooksPath`, and a
  command it cannot split is judged by whether its text holds `--no-verify`.
  The guard decides nothing about a commit; a call it does not recognise is
  not asked about. The answer is the JSON object both hosts
  read, `hookSpecificOutput` with `permissionDecision` and
  `permissionDecisionReason`: `ask` for Claude Code, `deny` for Codex, which
  does not support `ask`. In every other case it prints nothing and exits 0.
- **Without `cw`.** The guard command does nothing when `cw` is not on
  `PATH`. The git hooks still deny.
- **What the plugin does not do.** It reads no git command and decides about
  no commit or push.

## Platform

- **Windows locks:** `LockFileEx`, released by the system when the handle
  closes or the process dies.
- **Windows process tree:** a job object with kill-on-close. The program
  starts suspended, is assigned to the job and then continues, so every
  process it starts is in the job. The first CI run on Windows showed why:
  assigned after its start, a program had under load already ended or
  already started a child.
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
| The 154 scenarios | Tests that build `cw` and drive it and git in temporary repositories, written from the specifications by another agent than the implementer | Linux, locally | The behaviour on Linux. Scenarios that name Windows are written and skipped there. |
| No network | A test that lists the packages `cw` links for Linux, macOS and Windows. It finds no `net` for Linux and macOS. For Windows `net` is linked through `golang.org/x/sys/windows` alone, which `cw` calls for job objects and file locks; no package of `cw` imports it. | Linux | That no network code is linked on Linux and macOS, and that on Windows no code of `cw` reaches any; stronger than the scenario asks. |
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
| The machine loses power after a run ended | The finished record was synced before the rename, so it is whole or absent. |
| The clock is set back | The order of runs is their `seq`; a record dated in the future does not count. |
| A submodule holds uncommitted changes | `cw run` and `cw status` exit 1 and name it; a hook that denies says that no snapshot could be taken. |

## What the gate trusts

`cw`, the checks and the coding agent run as one user. `cw` trusts what is in
the git directory: its own records and mode file, the hook files, the refs
and the configuration. A process that sets out to write there can defeat the
gate, as it can with `git commit --no-verify`. The gate is a guardrail, and
the documents say so; CI is the backstop.

## Rollback

Everything is on one local branch. Deleting the branch and the worktree
restores the state before this work. `main` and the owner's installed `cw` are
untouched.

## Independent challenge

See the section of the same name in
[the production plan](../../production-plan.md): one reviewer read this design
and the role design; its findings and what was done about each are listed
there.
