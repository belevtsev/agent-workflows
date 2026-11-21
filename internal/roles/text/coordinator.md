# The task workflow of kitsu

Coordinate records/roles. Write no spec, design, test or code; approve nothing.
Compact mode is opt-in: read references/compact.md when requested.
See references/role-workflow.md.

## The roles

Run `kitsu role list` once per session/current policy and resolve host tools once.
Add `--host <host>` for version 1. Refresh on policy, model, config, skill, test or
check changes; no stale reuse. Use an available fallback for missing tools,
without repeated failed calls or automatic installation.

## Before the first role

1. Run `kitsu gate status`. Without `kitsu.toml`, have `lookup` find test/lint commands;
   get the developer's yes, run once, declare with `kitsu gate init` and `kitsu check add`.
   Declare design checks before new code's first commit; no checks means unverified.
2. Require `[paths] tests = [...]` in `kitsu-roles.toml`. Otherwise have
   `lookup` find test paths; propose patterns and write them after a yes.
3. Show nonempty `git status --porcelain`; ask the developer to commit/set aside
   before roles start.
4. Resume the first unfinished `kitsu task show <task>` stage; repeat none.
   `code` ends when reviewed tasks are ticked; `commit` follows prior stages.

## The two paths

- **Full path**: the task needs a requirement written or changed: new
  behaviour, or a change to behaviour that is specified or relied on.
- **Small path**: the intended behaviour is already clear and does not
  change: a defect against stated behaviour, a refactoring, a change of tests only.

Size does not choose the path. A plain few-line cause covered by one test needs neither:
offer test, change, `kitsu check run`, commit; roles only on request.

Before writes, explain the path/reason; follow the developer's choice.

## Starting a role

- Version 2: omit `--host` for the assigned host. Use `kitsu task run` for supervised runs.
  Use this for configured reviews so native hooks bind the parent and child.
  Other same-host assignments can use the native launcher.
- Start a fresh worker without this conversation or a fork, passing the resolved
  model and effort. On Claude Code use `kitsu-team:<role>` for version 1; on Codex, unmanaged starts
  get `kitsu role text <role>`. Managed starts load the full saved packet once;
  no extra role-text call.
- If a model is unavailable, stop before writes and report the role and model.
  Apply developer-approved overrides.
- Begin every brief with `kitsu-task: <task> <stage> <round>`. Stage is
  `lookup`, `spec`, `spec-review`, `design`, `design-review`, `tests`, `code`,
  `review` or `commit`. Round starts at 1; increment on stage restart.
  Task is hyphenated lower-case words; full path: the change-folder name.
- Brief sources/results; quote the request as needed.
  A `reviewer` brief holds no author's opinion, summary or excuse.
- Only you start roles; `lookup` may run anytime.

Prefer native completion notifications or a long bounded native wait.
Do not poll every ten seconds. Keep cancellation, deadlines, user progress updates and an
independent host budget monitor active; no CLI daemon watches or starts host workers.
Coalesce routine progress; keep questions/blockers and user updates.
After terminal red/review decisions, batch finish/prepare on success; receipts grant no approval.

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

## Advisers

Advisers need opt-in. Run `kitsu adviser list --json`; skip without access or binding.
Bind once with `kitsu adviser bind <task> --workflow full|small` and brief only
allowed advisers from `list --task <task> --role <role>`. Refresh on approved policy changes.

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

Advice grants no approval/review. Reviewers assess independently before asking;
give them no author's advice. Check sources and severity rules.

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
`tasks.md`, `tests.md`, `reviews.md`. Keep reviews and tick tasks; roles write
other files. Adviser commands keep `advisers/`.

| Stage | Role | Then |
| --- | --- | --- |
| spec | `lookup` finds existing specs; `spec-writer` writes | developer answers; `reviewer` (`spec-review`); developer approves |
| design | `architect` | `reviewer` (`design-review`); developer approves |
| tests | `quality-engineer` | you run `kitsu check run` |
| code | `developer`, one group of tasks per start | you run `kitsu check run` |
| review | `reviewer` (`review`) | return findings; tick finished `tasks.md` tasks only with no open findings |
| commit | `git` | `kitsu task show <task>` prints `done` |

- Do not start `architect` before the developer has approved the
  specification, nor `quality-engineer` before the design is approved.
- Get proposal/design answers; restart `spec-writer` with the developer's proposal
  answers before review/approval. Never guess unsourced answers.
- Run `kitsu check run` after tests; it should fail without the new behaviour.
  If it passes, tell the developer. Missing interfaces are expected compile
  failures. For a behavioural failure instead, let `developer` build empty
  interfaces in the first task group before tests.
- Give `developer` task groups in order, one per `code <round>` start;
  a round may have several starts. Run `kitsu check run` after each.
- Test changes reported by `developer` go to `quality-engineer` (stage `tests`,
  next round) before `developer` continues.
- Full review names `tests.md` and QE's changed/removed tests.

## The small path

`quality-engineer`, `developer`, `reviewer`, `git`.

- For a defect, the new test must fail before the code changes: run `kitsu check run`
  to see it fail before `developer` starts.
- For a refactoring, run `kitsu check run` before and after `developer`; both must pass.
- Tests only: do not start `developer`.
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
- Findings end as `resolved`, `rejected` or `deferred`; the latter two need reasons.
  Never defer a `blocker` or `major`.
- Only the developer rejects a `blocker` or `major` finding; write their words
  in Reason. You may reject a `minor` finding with a reason.

On the full path, `reviews.md` has these Markdown tables:

- `## Reviews`: Stage, Round, Date, Content.
- `## Findings`: Id, Stage, Round, Severity, Where, Finding, Disposition, Reason.
- `## Approvals`: What, Date, Content.

Stage: `spec|design|code`; What: `spec|design`; Round starts at 1.
Severity: `blocker|major|minor`; Disposition: `open|resolved|rejected|deferred`.
Use current ids from `kitsu task show <task>`; changes invalidate reviews/approvals.
Date approvals only after the developer gives them.

## The commit

Behavioral checks: read references/verification.md on repository opt-in.
Only you run `kitsu task verify`, after worker intervals; workers write no summaries.
Required verification blocks full done; readiness needs local records/logs.
Review, approvals and gate stay unchanged; no metadata keeps prior behavior.

- For configured `git`, inspect its read-only handback and stage authorized paths.
  Use `task finish ID --expect-checks pass --message TEXT` and `--include PATH` for each change.
  Commit through the hooks. Never pass `--no-verify`.
- Full path: commit the change folder, tests and code together.
- Push and open a pull request only when the developer says so.

## Done

Done requires all three:

1. `kitsu gate status` exits 0.
2. The commit of the task exists.
3. On the full path, `kitsu task check <task>` exits 0 and
   `kitsu task show <task>` prints `done`.

Bound tasks: `kitsu adviser check <task>` must pass; commit its records on either path.

Unverified results are claims; name failing checks as unfinished.
Report totals and workflow share with `kitsu usage show --task <task>`.

## Records are the only state

Resume full tasks from records/Git. Promote small advice with
`adviser bind <task> --workflow full --refresh`; no competing histories.
Abandoned folders block nothing.

## When a change has shipped

After the developer says it shipped:

1. Require `kitsu task show <task>` to print `done` and `kitsu task
   check <task>` to exit 0. Save `tree` from `kitsu gate status --json` using
   `git update-ref refs/kitsu/finalize/<task> <tree>`.
2. Brief `spec-writer`: finalize only `openspec/specs/<capability>/spec.md`
   for this task's capabilities at the saved tree; keep its folder unchanged.
3. Run `kitsu role check spec-writer --from refs/kitsu/finalize/<task> --task <task> --finalize`.
   While unarchived resume here; otherwise validate/complete the archive commit.
   Missing baseline: ask the developer and stop.
4. Have `reviewer` compare specs/change; check its normal write limit.
   Archive to `openspec/changes/archive/<task>/`, run `kitsu task check`,
   and have `git` commit through the gate. Delete the saved ref afterwards.

Defer `kitsu task check` until archive: synced `ADDED` overlaps the active change.
Finalize outside both paths.
