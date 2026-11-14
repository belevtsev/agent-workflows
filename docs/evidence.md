# Evidence

The gate allows a commit or a push when every declared check has a recorded
passing run on exactly the content that git records or sends. This page says
what such a run is, what "the same content" means, what a passing run proves
and what it does not, and when `cw run` executes a check again.

The examples on this page were produced on Linux with git 2.53. Temporary
paths are shortened to `<repository>`.

## A recorded passing run

`cw run` writes one record per run under `cw/runs/` in the common git
directory ([Operations](operations.md#where-cw-keeps-its-records) lists the
files). A record holds:

- the check's `id`, `run` and `paths`, as `cw.toml` declared them;
- `tree`: the snapshot of the working tree the run started on;
- `content_key`: the key of the checked content of that tree, explained below;
- the outcome, the exit status, the start and end times, and the path of the
  log;
- `seq`: the order in which the ends of runs on this content were recorded.

A run is passing when its outcome is `passed`: the program exited 0, and the
snapshot of the working tree after it equals the snapshot before it.

## The same content

### How content is identified

The content of a working tree is its snapshot: the git tree that `git add -A`
followed by `git commit` would record. `cw` computes it from a copy of the
index, so the index, `HEAD` and the files on disk do not change. The snapshot
holds:

- every tracked file as it is on disk, after git's own conversions: line
  endings, clean filters and `core.fileMode`;
- every untracked file that git does not ignore;
- a submodule as the commit it has checked out;
- a symbolic link as the link, not as the content it leads to.

It does not hold ignored files. A file listed in `.gitignore`, such as a build
directory, is not part of the content, and changing it changes nothing for
`cw`. A `cw.toml` that git ignores is not part of the content either, and the
tree then has no declaration.

There is no limit on the number or the size of the files: git hashes them.

The snapshot cannot be taken, and `cw run` and `cw status` exit 1 and name the
path, when git cannot record the working tree as it is on disk:

```text
cw: the working tree cannot be recorded: README.md is marked skip-worktree and differs from the index
```

The same holds for a file marked assume-unchanged that differs, a sparse
checkout that keeps tracked files off the disk, a submodule or nested
repository with uncommitted changes, a submodule that holds a path marked
assume-unchanged or skip-worktree, a submodule that is not checked out but
whose folder holds files, and any case in which `git add -A` fails, such as a
nested repository with no commit:

```text
cw: the working tree cannot be recorded: git add -A failed:
error: 'sub/' does not have a commit checked out
error: unable to index file 'sub/'
fatal: adding files failed
```

### What paths changes

Each check has a content key for a tree:

- A check without `paths` depends on every path. Its key is the id of the whole
  tree, so any change to any non-ignored file, `cw.toml` included, makes a new
  content that needs a new run.
- A check with `paths` depends only on the entries whose path matches a
  pattern, and on every submodule entry. Its key is the SHA-256 of those
  entries as `git ls-tree -r` lists them: mode, type, object id and path. Two
  trees hold the same checked content for the check exactly when these entries
  are the same. A tree holds a submodule as one entry, which no pattern for a
  file inside it matches, so a submodule entry counts in the key of every
  check that has `paths`: a new commit of any submodule needs a new run.

So for a check with `paths = ["src/**"]`:

- an edit of `README.md` keeps the check satisfied;
- an edit inside `src/`, a new file in `src/`, a file removed from `src/`, or a
  changed executable mark there needs a new run;
- a file moved out of `src/` needs a new run. Renames are not detected: a moved
  file differs at its old path and at its new one;
- content that was checked earlier is recognised again: after `src/app.txt` is
  changed and changed back, the earlier run counts again.

`paths` is a promise by the repository about what the check reads: nothing
else. `cw` does not check the promise. When a check reads a file outside its
`paths`, a pass is carried over to content the check never saw. A symbolic
link is recorded as a link, so the content behind it is not part of the key,
even when the link matches a pattern. A pattern that matches no entry gives the
same key for every tree that has no such entry, so one pass then counts for all
of them.

A pattern that names a folder of the tree matches nothing, because a pattern
matches files. Such a declaration is invalid, and `cw run`, `cw status` and a
hook say which pattern to write:

```text
cw: cw.toml:9: the pattern `src` of the check `folder` names a folder, and a pattern matches files; for everything in the folder write `src/**`
```

### The index against the working tree

`cw run` and `cw status` judge the snapshot of the working tree. A hook judges
the tree git is about to record: for a commit, the tree of the index.

When everything is staged, the two are the same tree. When part of the work is
not staged, the commit records other content than the one `cw run` checked,
and the hook denies the commit unless the difference lies outside the `paths`
of every check. `cw status` shows the difference before you commit:

```text
verified: every declared check has a passing run on this content
  unit  satisfied
  lint  satisfied
index: differs from the working tree; not staged:
  src/new.txt
a commit of the index alone would be denied: its content has no passing run of every check
  lint  not run on this content
stage everything with `git add -A`, or run the checks on what you will commit
```

## What a passing run proves

A passing run proves that, on this machine, the program of the check exited 0
when it started from the root of a working tree whose snapshot was that tree,
and that the snapshot after the run equals the snapshot before it. Only these
two snapshots are compared: a file that changed during the run and changed
back before it ended is not seen.

It does not prove:

- that the check tests anything. `cw` knows no language and reads no test
  report; a check whose program is `true` passes;
- anything about what the check does not test;
- anything about the environment: the version of a compiler or a tool, the
  environment variables, the ignored files, such as caches or a local
  configuration, the network, or the state of another machine. The program
  runs with the environment of the `cw run` that started it. To make the
  version of a tool part of the content, keep it in a committed file that the
  check reads, such as a lock file or a file of tool versions, and name that
  file in `paths` when the check has `paths`: a new version is then new
  content, and the check runs again;
- that `paths` is right: see [What paths changes](#what-paths-changes);
- that the record is genuine. The records are plain files in the git directory.
  A process that writes there can forge one. The gate trusts the git directory;
  it is a guardrail, not a defence against someone who decides to go around
  it;
- anything in another clone. Records are not shared between clones, and not
  with continuous integration, which runs the checks again
  ([Operations](operations.md#continuous-integration)).

## When a run is reused

For each selected check, `cw run` takes a snapshot, computes the check's content
key, and reads the records of that check and that key. The check is satisfied,
and `cw run` does not execute it again, when the run that decides is `passed`:

1. Only records with the same `id`, the same `run` and the same `paths`, in the
   same order, count. The `timeout` does not matter.
2. Among those, only decisive runs decide: `passed`, `failed` and `timed_out`.
   A run that ended `source_changed` or `interrupted`, or that is executing
   now, decides nothing.
3. The decisive run whose end was recorded last decides. The order is `seq`,
   not the clock, so a clock that was set back does not reorder runs. Of two
   runs with the same `seq`, the one that did not pass is the later.
4. That run decides only when it ended within the last 30 days, and not later
   than now. An older run never decides in its place, so a pass does not come
   back because a later failure aged out.

`cw run` executes the check again in every other case:

- the content changed, so no record exists for the new key;
- the most recent decisive run failed or timed out. A pass after a failure on
  the same content replaces the failure, and `cw stats` counts it as a pass
  after a failure;
- the deciding run is older than 30 days, or dated in the future;
- a record of this check and content cannot be read;
- the declaration of the check changed (see below).

```text
$ cw run
== unit: satisfied, not run again: run 20261114T144211Z-f48b9af5 passed on this content
== lint: true
== lint: passed in 0s
every declared check has a passing run on this content
```

`cw run` takes a new snapshot before each check. When an earlier check in the
same `cw run` changed the working tree, the next check runs on that changed
content, and the earlier one ends `source_changed`.

A check that writes a file into the working tree, such as a coverage report,
ends `source_changed`, also when the file lies outside its `paths`. The line
names the files and says what to do:

```text
== writes: sh -c echo r > coverage.out
== writes: source_changed: the source changed during the run: coverage.out; when the check writes these files, list them in .gitignore or write them outside the working tree
   log: <repository>/.git/cw/logs/20261114T175338Z-db4d8cac.log
not verified: 1 of 2 checks did not pass; fix what each log shows, then run `cw run` again
```

A second `cw run` can pass only because the file is then already there with
the same content; the check still writes into the working tree.

### --force

`cw run --force` executes the selected checks even when they are satisfied.
The new run becomes the most recent one: a `--force` run that fails makes the
check `failed` on that content, even after an earlier pass. `cw run <id>
--force` executes only the named checks.

### Timeouts

Each check has a timeout, `10m` unless `cw.toml` sets one from `1s` to `6h`.
When it passes, `cw` stops the program and every process it started, and the
run ends `timed_out`:

```text
== slow: sleep 5
== slow: timed_out after 1s: the check did not end within its timeout of 1s
   log: <repository>/.git/cw/logs/20261114T144258Z-6d1dadd1.log
not verified: 1 of 1 checks did not pass; fix what each log shows, then run `cw run` again
```

`timed_out` is decisive: the check is not satisfied on that content until a
later run passes. A process that the program left running when it exited is
stopped too. When such a process held the output open, `cw` prints a warning
that names the cause; the outcome stays the one the exit status gave.

When `cw run` itself receives SIGINT, SIGTERM or SIGHUP, or on Windows Ctrl+C,
it stops the program and records the run as `interrupted`. When the `cw` process dies without
recording an end, the next command reports that run as `interrupted`, and
nothing waits for it.

## When a check's declaration changes

The records of a check are filed under its `id` and its content key, and they
count only with the same `run` and `paths`. So when `cw.toml` changes:

| Change | Effect on the records |
| --- | --- |
| `run` changes | The old runs no longer count. The check is `not run` until it runs again. |
| `paths` changes, also only its order | The old runs no longer count. |
| `timeout` changes | The old runs still count. |
| `id` changes | The check has no records under the new id. |
| a check is removed | Its records stay until they age out; they count again if the same check comes back on the same content. |
| any edit of `cw.toml` | `cw.toml` is part of the tree, so every check without `paths`, and every check whose `paths` match `cw.toml`, needs a new run. |

When a commit changes or removes a check that the tree of `HEAD` declares, the
hook prints a notice; see [Hooks](hooks.md#pre-commit-and-pre-merge-commit).

## What cw status says

`cw status` judges the snapshot of the working tree with the same rules. It
executes nothing. Each check gets one state:

| State | Text in `cw status` | When |
| --- | --- | --- |
| `satisfied` | `satisfied` | The deciding run passed. |
| `not run` | `not run on this content` | No decisive run of this check counts on this content: none was made, the last one is older than 30 days or dated in the future, a record cannot be read, or the declaration changed. When the last run on this content decided nothing, the text adds it, such as `(the last run ended source_changed: the source changed during the run: coverage.out)`. |
| `failed` | `failed with exit status 3; log: <path>` | The deciding run failed. When the program could not be started, or did not exit on its own, the text says so and names it. |
| `timed out` | `timed out: the check did not end within its timeout of 1s; log: <path>` | The deciding run timed out. |

`(a run is executing now)` is added when a run of the check on this content is
executing. A record that cannot be read, and a run dated in the future, also
print a warning on standard error:

```text
cw: warning: the record <repository>/.git/cw/runs/lint/18a2e10dc060a52237948f45a54caf408e5eab3f/20261114T144923Z-d0538c16.json cannot be read (not a run record: unexpected end of JSON input); `lint` does not count as run on this content until it runs again
cw: warning: run 20261114T144923Z-ce79e12c of `unit` is dated in the future (2026-11-16T14:49:23Z); it does not count
```

The next run of that check on that content that ends decisively renames the
unreadable record to `<name>.damaged`, and it is ignored from then on. A record
dated in the future is not renamed; a later run outranks it by its `seq`.

The first line of `cw status` sums up the snapshot:

| First line | Exit status |
| --- | --- |
| `verified: every declared check has a passing run on this content` | 0 |
| `no checks are declared, so every commit is allowed` | 0 |
| `not verified: a commit of this content would be denied` | 5 |
| `not verified, but no hook of cw judges a commit in this clone yet` | 5 |

The last line is printed in a clone where the hooks of `cw` are not installed,
so git would allow the commit. A warning on standard error names `cw init`.

[Operations](operations.md#cw-status) describes the rest of the output and the
JSON form.
