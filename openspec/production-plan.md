# Production plan: the evidence gate and the roles

Written on 14 November 2026. The owner asked for this plan, said that nobody
can guide the implementation, and wants something to test on return. This file
is the overview. The engineering content is in the two changes:

| Part | Proposal | Requirements | Design | Tasks |
| --- | --- | --- | --- | --- |
| 1. The gate | [proposal](changes/reduce-cw-to-evidence-gate/proposal.md) | 54, with 144 scenarios | [design](changes/reduce-cw-to-evidence-gate/design.md) | [62 tasks](changes/reduce-cw-to-evidence-gate/tasks.md) |
| 2. The roles | [proposal](changes/add-role-workflow/proposal.md) | 44, with 82 scenarios | [design](changes/add-role-workflow/design.md) | [27 tasks](changes/add-role-workflow/tasks.md) |

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
  code.

## What is left for the owner

- Pushing, a pull request, a tag, a release, and every repository setting.
- Removing `_quarantine/` (task 11.1 of part 1).
- Running anything with a model: the evaluation and the comparison with one
  agent (part 2, group 7). Until then no document claims that the roles
  improve anything.
- Confirming the behaviour on macOS, on Windows and on Codex. The build
  machine is Linux, and CI does not run for a branch that is not pushed.

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

Everything is on one local branch in one worktree. Removing the worktree and
the branch restores the state of 14 November 2026. Nothing else changed.

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

## Changes to the specifications during the build

| Date | Where | What changed | Why |
| --- | --- | --- | --- |
| 14 Nov 2026 | `check-evidence`: Working tree that cannot be recorded | Also refuses a submodule with uncommitted changes; one scenario added | Challenge finding 3 |
| 14 Nov 2026 | `check-evidence`: Satisfied check, Old runs | Order of runs is the order of recording, not the clock; a run dated in the future does not count; two scenarios added | Challenge finding 2 |
| 14 Nov 2026 | `check-evidence`: Damaged and concurrent records | An unreadable record makes its check not satisfied for that content until it runs again; records are synced; one scenario added | Challenge finding 4 |
| 14 Nov 2026 | `publish-gate`: Push rule | One scenario added: an annotated tag | Challenge finding 10 |
| 14 Nov 2026 | `gate-reporting`: Status; new requirement Gate state | The index file is not rewritten (one scenario); `cw status` reports hooks git does not run (two scenarios) | Challenge findings 12 and 7 |
| 14 Nov 2026 | Part 1 totals | 54 requirements and 144 scenarios, from 53 and 136 | The rows above |
| 14 Nov 2026 | `check-declaration`: fixture, the meaning of "Refused" | "Writes nothing" now says what it covers: no new or changed file or directory in the working tree, the home directory or the git directory; objects git stores for a snapshot are not counted | Three scenario tests failed because a refused `cw run` and `cw status` left an empty `cw/` directory. The code was fixed; the wording was made exact |

## Progress

| Milestone | State | Commit | Evidence |
| --- | --- | --- | --- |
| M1 | done | `4fff8ad` | The two change folders and this plan are on the branch. No pull request: publishing is the owner's. |
| M2 | done | `cb59433` | Local, Linux: `go build`, `go vet`, `go test -race` pass; 2,216 files hold the same blob under `_quarantine/`; `cw version` exits 0 and `cw nosuch` exits 2; `claude plugin validate .` passes. Task 2.4 is written but not ticked: its check is a CI run. |
| M3 | done, Linux only | `df3f3b5`, tests in the commit after it | Local, Linux: the 62 scenarios of tasks 5.3 to 5.5 each have a test, written from the specifications by agents that did not read the code. 58 pass; 3 need Windows and are skipped; 1 waits ten minutes and runs only with `CW_SLOW_TESTS=1`, and has never run. Three tests failed first (a refused command left an empty `cw/` directory); the code was fixed. Tasks 3.x to 5.x are not ticked where their check names the three systems. |
| M4 | built, tests being written | the commit after `df3f3b5` | Local, Linux, by hand in a demo repository: an unchecked commit is denied and a checked one recorded; a commit that leaves a file out names the file; a push of a commit made around the hook is denied; a push of another branch says where to run `cw run`; a tag on a commit the remote has, a new branch at that commit and a deletion go through; observe mode prints one line; a missing `cw` and an unreadable store deny; `cw uninstall` removes the hooks and the store. |
| M5 | not started | | |
| M6 | not started | | |
| M7 | not started | | |
| M8 | not started | | |
