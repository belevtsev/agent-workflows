# Production plan: the evidence gate and the roles

Written on 14 November 2026. The owner asked for this plan, said that nobody
can guide the implementation, and wants something to test on return. This file
is the overview. The engineering content is in the two changes:

| Part | Proposal | Requirements | Design | Tasks |
| --- | --- | --- | --- | --- |
| 1. The gate | [proposal](changes/reduce-cw-to-evidence-gate/proposal.md) | 53, with 136 scenarios | [design](changes/reduce-cw-to-evidence-gate/design.md) | [62 tasks](changes/reduce-cw-to-evidence-gate/tasks.md) |
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

| # | Finding | Severity | Disposition | What was done |
| --- | --- | --- | --- | --- |

The table is filled in when the review returns.

## Changes to the specifications during the build

| Date | Where | What changed | Why |
| --- | --- | --- | --- |

## Progress

| Milestone | State | Commit | Evidence |
| --- | --- | --- | --- |
| M1 | done | `4fff8ad` | The two change folders and this plan are on the branch. No pull request: publishing is the owner's. |
| M2 | done | this commit | Local, Linux: `go build`, `go vet`, `go test -race` pass; 2,216 files hold the same blob under `_quarantine/`; `cw version` exits 0 and `cw nosuch` exits 2; `claude plugin validate .` passes. Task 2.4 is written but not ticked: its check is a CI run. |
| M3 | not started | | |
| M4 | not started | | |
| M5 | not started | | |
| M6 | not started | | |
| M7 | not started | | |
| M8 | not started | | |
