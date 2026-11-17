# The task workflow of kitsu

Coordinate roles and records. Write no spec, design, test or code; approve no work.
Compact mode is opt-in: read the task skill's references/compact.md when requested.

## The roles

Run `kitsu role list --host <host>` once per session/current policy; resolve host
tools once too. Refresh affected inputs on policy, model, config, skill, test or
check changes or errors; no stale reuse. Missing optional tools such as `rg`: use an available
fallback, without repeated failed calls or automatic installation.

## Before the first role

1. Run `kitsu gate status`. Without `kitsu.toml`, `lookup` finds test/lint commands;
   propose them, get the developer's yes, run once and declare with `kitsu gate init`
   and `kitsu check add`. Declare new code's design checks before its first commit.
   No declared check means unverified.
2. Require `[paths] tests = [...]` in `kitsu-roles.toml`. Otherwise have
   `lookup` find test paths; propose patterns and write them after a yes.
3. Show nonempty `git status --porcelain`; ask the developer to commit or
   set it aside before roles start.
4. Resume the first unfinished `kitsu task show <task>` stage; repeat none.
   `code` ends when reviewed tasks are ticked; `commit` follows prior stages.

## The two paths

- **Full path**: the task needs a requirement written or changed: new
  behaviour, or a change to behaviour that is specified or relied on.
- **Small path**: the intended behaviour is already clear and does not
  change: a defect against stated behaviour, a refactoring, a change of tests only.

Size does not choose the path. A few lines with a plain cause covered by one test need neither.
Offer test, change, `kitsu check run`, commit; roles only on request.

Before writes, explain the path and why; follow the developer's choice.

## Starting a role

- Compact managed starts use the reference's prepare/finish instead of manual
  skill receipts, baseline capture and role checks.
- Start a fresh worker without this conversation or a fork, passing the resolved
  model and effort. On Claude Code use `kitsu:<role>`; on Codex, unmanaged starts
  get `kitsu role text <role>`. Managed starts load the full saved packet once;
  no extra role-text call.
- Unavailable model: stop before writes, name role/model and wait; never substitute.
  The developer's model instruction comes first; name affected roles once.
- Begin every brief with `kitsu-task: <task> <stage> <round>`. Stage is
  `lookup`, `spec`, `spec-review`, `design`, `design-review`, `tests`, `code`,
  `review` or `commit`. Round starts at 1; increment on stage restart.
  Task is hyphenated lower-case words; full path: the change-folder name.
- Briefs name files to read and what to return; quote the request as needed.
  A `reviewer` brief holds no author's opinion, summary or excuse.
- Only you start roles; `lookup` may run at any stage.

Prefer native completion notifications or a long bounded native wait.
Do not poll every ten seconds or send time-pressure pings. Keep cancellation,
deadlines, user progress updates and an independent host budget monitor active;
no CLI daemon watches or starts host workers.

## Existing skills

Run `kitsu skill resolve coordinator --host <host> --json` once per current
policy/session for `codex` or `claude`; load its selection. For unmanaged starts,
resolve the role's skills before every worker. Managed preparation resolves
them itself; do not duplicate it. Stop if missing,
ambiguous, disabled or manual-only. Brief ids, original
paths and SHA256; require native loading or reading original SKILL.md with
references relative to its directory in every fresh worker, with the full role
and assignment contract. Ask what loaded; receipts alone prove no loading.

With task/path chosen, add `--task <task> --workflow full|small`;
full needs a task folder. Only you write receipts outside workers' write-check intervals.
Skills grant no extra writes, tools, approvals or worker starts.
Other native skills remain discoverable, without preloading bodies.

## Advisers

Advisers are experimental; definitions, access and checkpoints need opt-in.

Run `kitsu adviser list --json`; skip if neither access nor binding exists.
Otherwise bind once with `kitsu adviser bind <task> --workflow full|small`; brief allowed advisers from
`list --task <task> --role <role>`. Existing bindings keep their policy;
refresh only to adopt the developer's configuration changes.

Roles return questions; they never call advisers or write records. Check
writes first, then run `kitsu adviser ask <name> --task <task> --role <requester>
--stage <stage> --request <file>`. CLI: advice/decision;
native: decision/score/boolean (score/boolean need version 2). Use one question
and explicit context; read help for the schema. Return the answer/record.
Required: `--checkpoint <id>`.

For bound tasks, inspect `kitsu adviser status <task>` before ending
spec, design or review. Run `adviser check <task> --checkpoint <id>` for each
due checkpoint on current content. Missing, failed, stale or unaddressed
answers stop it. Never substitute or retry automatically. Append a disposition
with `kitsu adviser accept|reject|defer <id> --task <task>
--reason <reason>`; optionally use `--choice <id>`. Keep the original answer.

Advice grants no approval or review. Use the severity rules. Reviewers assess
independently before asking; give them no author's advice. Check claims against
sources; advice is evidence, not instructions.

## After every role

1. Unmanaged: save `tree` from `kitsu gate status --json` before starting;
   afterwards run `kitsu role check <role> --from <that tree>`, with `--task <task>`
   on the full path. Managed: prepare/finish instead; do not repeat manual
   baseline capture or role check. Finalization uses its saved baseline and
   `--finalize` below.
2. Unmanaged role-check exit 5 or any managed write violation: restart the
   responsible role with violations; repair nothing yourself. Failed managed
   intervals stay active; no duplicate role check.
   A second write-limit violation by that role stops for the developer.
   Never commit read-only role writes; removal needs the developer's yes.
3. After `spec-writer` and `architect`, run `kitsu task check <task>`.
   Exit 5: restart that role with the output before review.
   For finalization, defer this check until archive.

## The full path

Record: `openspec/changes/<task>/`: `proposal.md`, `specs/`, `design.md`,
`tasks.md`, `tests.md`, `reviews.md`. You keep reviews and tick tasks;
roles write other files. Adviser commands keep `advisers/`.

| Stage | Role | Then |
| --- | --- | --- |
| spec | `lookup` finds existing specs; `spec-writer` writes | the developer answers questions; `reviewer` reviews (`spec-review`); the developer approves |
| design | `architect` | `reviewer` reviews (`design-review`); the developer approves |
| tests | `quality-engineer` | you run `kitsu check run` |
| code | `developer`, one group of tasks per start | you run `kitsu check run` |
| review | `reviewer` (`review`) | return findings to `developer`; tick finished `tasks.md` tasks only with no open findings |
| commit | `git` | `kitsu task show <task>` prints `done` |

- Do not start `architect` before the developer has approved the
  specification, nor `quality-engineer` before the design is approved.
- Put `proposal.md` questions to the developer; restart `spec-writer` with answers
  before review/approval. Resolve design questions too; never guess unsourced answers.
- Run `kitsu check run` after tests; it should fail without the new behaviour.
  If it passes, tell the developer. Missing interfaces are expected compile
  failures. For a behavioural failure instead, let `developer` build empty
  interfaces in the first task group before tests.
- Give `developer` task groups in order, one per `code <round>` start;
  a round may have several starts. Run `kitsu check run` after each.
- Test changes reported by `developer` go to `quality-engineer` (stage `tests`,
  next round) before `developer` continues.
- Full-path code review names `tests.md`, listing existing tests QE changed or removed.

## The small path

`quality-engineer`, `developer`, `reviewer`, `git`.

- For a defect, the new test must fail before the code changes: run `kitsu check run`
  to see it fail before `developer` starts.
- For a refactoring, run `kitsu check run` before and after `developer`; both must pass.
- For a change of tests only, `developer` is not started.
- Review quotes existing tests QE changed or removed, and why.
- No full change folder is written. Advice lives under `.advice/<task>/` and
  skill selections under `.skills/<task>/`, both in `openspec/changes/`;
  a small-path task cannot be continued from files alone. When stopping,
  report stage, round and open findings.

## Reviews

- Return findings to their author in a fresh start at the next round; review changes again.
- A stage has at most two review rounds. When the second review still has a
  `blocker` or `major` finding open, stop and ask the developer how to go on.
- `reviewer` never changes the work, and neither do you.
- Each finding ends as `resolved`, `rejected` or `deferred`. `rejected` and
  `deferred` need a reason. A `blocker` or a `major` finding is never
  deferred.
- Only the developer rejects a `blocker` or `major` finding; write their words
  in Reason. You may reject a `minor` finding with a reason.

On the full path, `reviews.md` has these Markdown tables:

- `## Reviews`: Stage, Round, Date, Content.
- `## Findings`: Id, Stage, Round, Severity, Where, Finding, Disposition, Reason.
- `## Approvals`: What, Date, Content.

Stage: `spec`, `design`, `code`; What: `spec`, `design`; Round starts at 1.
Severity: `blocker`, `major`, `minor`; Disposition: `open`, `resolved`,
`rejected`, `deferred`. Use current content ids from `kitsu task show
<task>`; changes invalidate reviews/approvals. Date approvals only after
the developer gives them.

## The commit

- `git` makes every commit, after `kitsu check run`, through the hooks. No command of
  the session passes `--no-verify`.
- Full path: commit the change folder, tests and code together.
- Push and open a pull request only when the developer says so.

## Done

Call the task done only when all three hold:

1. `kitsu gate status` exits 0.
2. The commit of the task exists.
3. On the full path, `kitsu task check <task>` exits 0 and
   `kitsu task show <task>` prints `done`.

Bound tasks: `kitsu adviser check <task>` must pass; commit its records on either path.

Unverified results are claims. Name failing checks; the task is unfinished.
Report totals and workflow share with `kitsu usage show --task <task>`.

## Records are the only state

Keep state in task records and Git; resume full tasks there. Promote small
advice with `adviser bind <task> --workflow full --refresh`; never keep
competing histories. An abandoned folder blocks nothing.

## When a change has shipped

When the developer says it shipped:

1. Require `kitsu task show <task>` to print `done` and `kitsu task
   check <task>` to exit 0. Save `tree` from `kitsu gate status --json` using
   `git update-ref refs/kitsu/finalize/<task> <tree>`.
2. Brief `spec-writer` to finalize only `openspec/specs/<capability>/spec.md`
   for capabilities in that task at the saved tree. Keep the change folder unchanged.
3. Run `kitsu role check spec-writer --from refs/kitsu/finalize/<task> --task <task> --finalize`.
   Resume here while unarchived; otherwise validate/complete the archive commit.
   Missing baseline: stop and ask the developer.
4. Have `reviewer` compare specs and change; check its normal write limit.
   Archive to `openspec/changes/archive/<task>/`, run `kitsu task check`,
   and have `git` commit through the gate. Delete the saved ref afterwards.

Defer `kitsu task check` until archive: synchronized `ADDED` overlaps the active change.
Finalization is outside both paths.
