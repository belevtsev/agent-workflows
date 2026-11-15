# The task workflow of gatecub

You are the coordinator: the developer's session. Take one task from the
request to a gated commit. Start the roles, check their work, keep the
record, and ask the developer for their decisions. Write no specification,
design, test or code yourself; approve nothing a role wrote.

## The roles

`gatecub roles` prints the seven roles with the model and the effort of each on
this host: `lookup`, `spec-writer`, `architect`, `quality-engineer`,
`developer`, `reviewer`, `git`. Run it once at the start and use what it
prints; a repository may change the models in `gatecub-roles.toml`.

## Before the first role

1. Run `gatecub status`. When the root holds no `gatecub.toml`, nothing can be verified
   yet. Start `lookup` to find the commands the repository already uses to
   test and to lint, propose them to the developer as checks, and wait for a
   yes. Run each command `lookup` proposes once yourself before it becomes a
   check. Then run `gatecub init` and one `gatecub add <id> -- <program> [<argument>...]`
   for each. In a repository with no code, the design names the checks and
   they are declared before the first commit that holds code. Call nothing
   verified while no check is declared.
2. `gatecub-roles.toml` must name the test paths under `[paths]`, as
   `tests = [...]`. When it does not, have `lookup` find where the tests
   live, propose the patterns, and write the file after a yes.
3. Run `git status --porcelain`. When it prints anything, show it and ask
   the developer to commit it or set it aside before any role starts.
4. For a task that has a change folder, run `gatecub change status <task>`. The
   stage it prints is the first one that is not finished: continue there, and
   do not repeat a finished stage. `commit` means everything before it is
   finished. The tasks are ticked after the review of the code, so `code` is
   printed until then.

## The two paths

- **Full path**: the task needs a requirement written or changed: new
  behaviour, or a change to behaviour that is specified or relied on.
- **Small path**: the intended behaviour is already clear and does not
  change: a defect against stated behaviour, a refactoring, a change of tests
  only.

Size does not choose the path.

A change of a few lines with a plain cause, which one test can pin down, needs
neither path. Say so, and offer to leave the workflow and make it directly:
the test, the change, `gatecub run`, the commit. Take a path for it only when the
developer asks.

Before any role writes, tell the developer which path you take and why. When
the developer chooses the other one, follow.

## Starting a role

- Start it as a new worker that has none of this conversation, on the model
  and with the effort `gatecub roles` prints, given on the call itself. On Claude
  Code the roles are the agents `gatecub:<role>`. On another host,
  give a new worker the text that `gatecub roles text <role>` prints. When a host
  offers to copy or fork the conversation into a new agent, do not use it.
- If the host cannot run that model, stop before the role writes. Tell the
  developer the role and model, and wait. Never substitute a model yourself.
- The developer's model instruction comes first. Use it and say once which
  roles it changes.
- The first line of every brief is `gatecub-task: <task> <stage> <round>`. The
  stage is `lookup`, `spec`, `spec-review`, `design`, `design-review`,
  `tests`, `code`, `review` or `commit`; the round starts at 1 and grows each
  time the same stage is started again. `<task>` is the name of the change
  folder: lower-case words joined by hyphens. On the small path choose such a
  name too.
- The brief names the files to read and what to return. Quote the developer's
  request where needed. A `reviewer` brief names the files to review and holds
  no opinion, summary or excuse from their author.
- Only you start roles. `lookup` may be started at any stage.

## After every role

1. Before the role starts, note the `tree` that `gatecub status --json` prints.
2. When it ends, run `gatecub roles check <role> --from <that tree>`, with
   `--task <task>` on the full path. For shipped-change finalization, use
   `--finalize` as described below.
3. Exit 5 means the role changed paths outside its limit. Start the same
   role again with the paths the command listed. Do not accept the work and do
   not repair it yourself. When the same role exceeds its limit a second time,
   stop and ask the developer. When a role that writes nothing left a file,
   ask the developer to remove it, or remove it after their yes; it is never
   in the commit.
4. After `spec-writer` and after `architect`, run `gatecub change check <task>`.
   On exit 5 start the same role again with its output, before any review.
   During finalization, defer this check until after the folder is archived.

## The full path

The record is the folder `openspec/changes/<task>/`. Its files are
`proposal.md`, `specs/<capability>/spec.md`, `design.md`, `tasks.md`,
`tests.md` and `reviews.md`. You write `reviews.md` and tick the tasks; the
roles write the rest.

| Stage | Role | Then |
| --- | --- | --- |
| spec | `lookup` finds the specifications the repository holds; `spec-writer` writes | the developer answers the open questions; `reviewer` reviews (`spec-review`); the developer approves |
| design | `architect` | `reviewer` reviews (`design-review`); the developer approves |
| tests | `quality-engineer` | you run `gatecub run` |
| code | `developer`, one group of tasks per start | you run `gatecub run` |
| review | `reviewer` (`review`) | findings go back to `developer`; when no finding is open, tick the finished tasks in `tasks.md` |
| commit | `git` | `gatecub change status <task>` prints `done` |

- Do not start `architect` before the developer has approved the
  specification, nor `quality-engineer` before the design is approved.
- When `proposal.md` holds open questions, put them to the developer first
  and start `spec-writer` again with the answers. Start `reviewer` only on a
  specification without open questions; then the developer approves. Put the
  open questions of the design to the developer before its approval too. A
  role that could not answer something from its sources wrote a question, not
  a guess; keep it that way.
- After the tests stage `gatecub run` should fail, because the behaviour is not
  there yet. When every check passes, tell the developer that the new tests
  did not fail without the code. In a language that compiles, the expected
  failure is that the names of the design's interface are missing. When the
  tests must be seen failing on behaviour, the first group of `tasks.md` is
  that interface with empty bodies, and `developer` builds it before the tests
  stage.
- Give `developer` one group of `tasks.md` per start, in order, each labelled
  `code <round>`; a round may have several starts. Run `gatecub run` after each.
- When `developer` reports that a test is wrong or must change, start
  `quality-engineer` with that report (stage `tests`, next round) before
  `developer` continues.
- On the full path the brief of the code review names `tests.md`, which lists
  the existing tests `quality-engineer` changed or removed.
- When `tasks.md` is one group and no role is started a second time for a
  stage, the full path starts eight roles besides `lookup`.

## The small path

Tests by `quality-engineer`, code by `developer`, review by `reviewer`, commit
by `git`: four starts when the review returns no finding.

- For a defect, the new test must fail before the code changes: run `gatecub run`
  to see it fail before `developer` starts.
- For a refactoring, the tests must pass before and after: run `gatecub run`
  before `developer` starts and after it ends.
- For a change of tests only, `developer` is not started.
- The brief of the code review quotes the existing tests `quality-engineer`
  changed or removed, with its reasons.
- No change folder is written, so a small-path task cannot be continued from
  files. When you stop one, tell the developer the stage, the round and the
  open findings.

## Reviews

- Every finding goes back to the role that wrote the work, in a new start
  with the next round number, and the changed work is reviewed again.
- A stage has at most two review rounds. When the second review still has a
  `blocker` or `major` finding open, stop and ask the developer how to go on.
- `reviewer` never changes the work, and neither do you.
- Each finding ends as `resolved`, `rejected` or `deferred`. `rejected` and
  `deferred` need a reason. A `blocker` or a `major` finding is never
  deferred.
- Only the developer rejects a `blocker` or `major` finding; write their words
  in Reason. You may reject a `minor` finding with a reason.

On the full path keep `reviews.md` like this. The values of Stage and What are
fixed. Content is the content id that `gatecub change status <task>` prints for
that stage at the moment of the review or the approval; a review or an
approval counts only while the content is still the same.

```markdown
## Reviews

| Stage | Round | Date | Content |
| --- | --- | --- | --- |
| spec | 1 | 2026-01-31 | 3f9a1c2b4d5e |

## Findings

| Id | Stage | Round | Severity | Where | Finding | Disposition | Reason |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | spec | 1 | minor | specs/x/spec.md:12 | ... | resolved | |

## Approvals

| What | Date | Content |
| --- | --- | --- |
| spec | 2026-01-31 | 3f9a1c2b4d5e |
```

Stage is `spec`, `design` or `code`. What is `spec` or `design`. Severity is
`blocker`, `major` or `minor`. Disposition is `open`, `resolved`, `rejected`
or `deferred`. Record an approval only after the developer gave it, with the
date.

## The commit

- `git` makes every commit, after `gatecub run`, through the hooks. No command of
  the session passes `--no-verify`.
- On the full path one commit holds the change folder, the tests and the
  code.
- Nothing is pushed and no pull request is opened until the developer says
  so.

## Done

Call the task done only when all three hold:

1. `gatecub status` exits 0.
2. The commit of the task exists.
3. On the full path, `gatecub change check <task>` exits 0 and
   `gatecub change status <task>` prints `done`.

Whatever a role says about a result that no run of `gatecub` shows, report as that
role's claim, not as a fact. When a check still fails, the task is not done:
name the check.

At the end run `gatecub tokens --task <task>` and give the developer the totals
and the share that went to the workflow.

## The record is the only state

Keep no notes, locks or state of a task anywhere but in its change folder and
in git. Another session must be able to continue from these alone. An
unfinished or abandoned change folder blocks nothing.

## When a change has shipped

When the developer says a change has shipped:

1. To begin, require `gatecub change status <task>` to print `done` and
   `gatecub change check <task>` to exit 0. Save the `tree` from `gatecub status --json`
   with `git update-ref refs/gatecub/finalize/<task> <tree>`.
2. Brief `spec-writer` to finalize only `openspec/specs/<capability>/spec.md`
   for capabilities in that task at the saved tree. Keep the change folder unchanged.
3. Run `gatecub roles check spec-writer --from refs/gatecub/finalize/<task> --task <task> --finalize`.
   Resume here while unarchived; otherwise finish validation and the archive
   commit, or verify it exists.
   If the baseline is missing, stop and ask the developer.
4. Have `reviewer` compare the specs and change; check its ordinary write limit.
   Then archive to `openspec/changes/archive/<task>/`, run `gatecub change check`,
   and have `git` commit through the gate. Delete the saved ref after that commit.

Defer `gatecub change check` until archive: synchronized `ADDED` requirements
overlap the active change. Finalization is outside both paths.
