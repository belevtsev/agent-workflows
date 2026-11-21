# Evaluation

## What this is and its state

Some scenarios of the role workflow need a model run inside Claude Code or Codex, so no test of
this repository can check them. Their heading says "(evaluation)". This document turns each
of them into a case that the repository owner runs by hand, and it defines the comparison with
one agent.

No case has been run, and no comparison has been run. Nothing here shows that the workflow works
as specified, and nothing here shows a benefit of the workflow. The owner runs the cases and pays
for the model runs. The result of each case goes into its own "Result" and "Date" fields below.

There are 26 such scenarios: 23 in
[task-flow](../openspec/changes/add-role-workflow/specs/task-flow/spec.md) and 3 in
[role-definitions](../openspec/changes/add-role-workflow/specs/role-definitions/spec.md). They are
cases E1 to E26; E26 was added later and stands with its requirement, "Two paths". The comparison comes from the requirement "Published comparison" of
[token-report](../openspec/changes/add-role-workflow/specs/token-report/spec.md) and from Success
measure 7 of the [proposal](../openspec/changes/add-role-workflow/proposal.md).

## Setup

### Three repositories

Success measure 1 asks for three repositories in three different languages. Call them A, B and C.
Each one must be:

- **Small.** One program that prints `Hello, <name>.` for a name it is given, in a few source
  files.
- **Tested.** It has a test command and a lint command of its own, in a script or written down in
  its own files, so that `lookup` can find them (E18 needs this).
- **Declared.** `kitsu.toml` at its root declares two checks, made with `kitsu init` and then
  `kitsu add unit -- <program> [<argument>...]` and `kitsu add lint -- <program> [<argument>...]`.
  The check ids are `unit` and `lint`.
- **Gated.** The hooks are installed with `kitsu init` in every clone that is used.
- **Roles file.** `kitsu-roles.toml` at its root names the test paths:

  ```toml
  version = 1

  [paths]
  tests = ["<the patterns of the repository's test files>"]
  ```

- **One existing test file** inside those test paths.
- **One planted defect.** The program accepts an empty name and prints `Hello, .`. No test covers
  this.
- **A remote.** A bare repository on the same machine is its remote `origin`, and the prepared
  state is pushed to it (E10 needs this).
- **Clean.** Everything is committed, and `kitsu status` exits 0 on the prepared state.

The specifications are written for a repository called `demo`. The cases use these names for the
same things in A, B and C:

| In the specifications | In the cases |
| --- | --- |
| `unit.sh` | the check `unit` |
| `lint.sh` | the check `lint` |
| `src/app.txt` | `<src>`: the source file that holds the greeting |
| `tests/app_test.txt` | `<test>`: the existing test file |
| "a greeting that `src/app.txt` does not hold yet" | the greeting `Good evening, <name>.` |

Each case starts from a fresh clone of the prepared repository with `kitsu init` run in it, unless
the case says that it continues from another case. Run each case once in each of A, B and C.

### Host

- **Claude Code**, with both plugins of this repository installed. Its roles are the agents
  `kitsu-team:<role>`.
- **Codex is unconfirmed.** Nothing was checked on Codex, and how Codex starts a role on a given
  model is not confirmed (proposal, Known limits and Open Question 12). Run no case on Codex
  until that is settled.
- No setting of the owner's own may override the model of a role. The proposal names
  `CLAUDE_CODE_SUBAGENT_MODEL_FORCE`: leave it unset.

### Starting a run

In Claude Code, at the root of the clone, give:

```text
/kitsu-team:managed <request>
```

`<request>` is the text in the case's "Request" field, word for word. The owner plays the
developer: approve when asked, answer open questions in the sense of the request, and say nothing
about a push, unless the case says otherwise.

### Where to look afterwards

- `kitsu change status <task>` and `kitsu change check <task>` for the change folder.
- `kitsu status` for the gate.
- `kitsu tokens --task <task> --json` for the role starts, their stages, rounds and models. Its
  `sessions` entry names the `record`: the host's record of the session.
- **The session's record.** It shows the coordinator's messages, the commands it ran with their
  output, and every role start in order. The first line of each role's brief is its label,
  `kitsu-task: <task> <stage> <round>`. The records of the roles sit beside it, in the session's
  `subagents` folder.
- `openspec/changes/<task>/reviews.md` for reviews, findings and approvals.
- `git log`, `git show` and `git diff` for what was committed.

`<task>` is the name of the change folder. The requests of the full path name it `add-greeting`,
as the specifications do. On the small path the coordinator chooses a name; `kitsu tokens --json`
lists it under `tasks`.

Several cases can be judged from the same run, for example E3, E4, E10, E13, E17, E21 and E25 from
one run of the full path. Record each case in its own fields all the same.

## The cases

The fixture of each specification applies as its "Shared fixture" says, with the names of the
table above.

### task-flow: Entry

#### E1. A folder with an approved specification

- **Scenario:** "A folder with an approved specification (evaluation)"; capability `task-flow`,
  requirement "Entry".
- **Prepare:** Run the request of E3 and end the session after the developer approved the
  specification and before `architect` starts. Commit the folder `openspec/changes/add-greeting/`
  and note the commit id as `<prepared>`. Check that `kitsu change status add-greeting` prints
  `design`.
- **Request:** `Continue the task add-greeting.`
- **Look for:**
  1. In the session's record, the first role started is `architect`, with the label
     `kitsu-task: add-greeting design 1`.
  2. No start is labelled `spec` or `spec-review`.
  3. `git diff <prepared> -- openspec/changes/add-greeting/proposal.md
     openspec/changes/add-greeting/specs/` prints nothing, on the working tree and on `HEAD`.
- **Result:** not run
- **Date:** not run

### task-flow: Two paths

#### E2. A defect takes the small path

- **Scenario:** "A defect takes the small path (evaluation)"; capability `task-flow`, requirement
  "Two paths".
- **Prepare:** The prepared repository with its planted defect. List `openspec/changes/`.
- **Request:** `The program accepts an empty name and prints "Hello, .". It should refuse an
  empty name with an error. Please fix it.`
- **Look for:**
  1. In the session's record, a message of the coordinator names the small path and a reason,
     and it comes before the first role start.
  2. `openspec/changes/` holds the same folders as before; `kitsu change status` lists no new one.
- **Result:** not run
- **Date:** not run

#### E3. New behaviour takes the full path

- **Scenario:** "New behaviour takes the full path (evaluation)"; capability `task-flow`,
  requirement "Two paths".
- **Prepare:** The prepared repository. `<src>` holds no evening greeting.
- **Request:** `Add an evening greeting: for a name, the program also prints "Good evening,
  <name>." Name the task add-greeting.`
- **Look for:**
  1. In the session's record, a message of the coordinator names the full path and a reason,
     and it comes before the first role start.
- **Result:** not run
- **Date:** not run

#### E26. A change of one line needs no roles

- **Scenario:** "A change of one line needs no roles (evaluation)"; capability `task-flow`,
  requirement "Two paths".
- **Prepare:** The prepared repository. Pick one word of a message the program prints and
  misspell it in `<src>`; commit that through the gate.
- **Request:** `/kitsu-team:managed The message has a typo: "<the misspelt word>". Fix it.`
- **Look for:**
  1. In the session's record, a message of the coordinator says that the task needs no roles
     and offers to make the change directly, and it comes before any role start.
  2. No role is started, unless the next message of the person who runs the case asks for a
     path.
- **Result:** not run
- **Date:** not run

### task-flow: Stages of the full path

#### E4. The order of the roles

- **Scenario:** "The order of the roles (evaluation)"; capability `task-flow`, requirement
  "Stages of the full path".
- **Prepare:** As E3.
- **Request:** The request of E3.
- **Look for:**
  1. `openspec/changes/add-greeting/reviews.md` holds no row under `## Findings`, `tasks.md` has
     one group, and no role was started a second time for a stage. If one of these fails, the
     WHEN does not hold: write "not triggered" as the result.
  2. In the session's record, the role starts other than `lookup` are, in this order:
     `spec-writer`, `reviewer`, `architect`, `reviewer`, `quality-engineer`, `developer`,
     `reviewer`, `git`.
  3. Their labels carry the stages `spec`, `spec-review`, `design`, `design-review`, `tests`,
     `code`, `review` and `commit`, each with round 1.
- **Result:** not run
- **Date:** not run

### task-flow: Stages of the small path

#### E5. A defect is shown by a test first

- **Scenario:** "A defect is shown by a test first (evaluation)"; capability `task-flow`,
  requirement "Stages of the small path".
- **Prepare:** As E2. Note the commit id of the prepared state as `<prepared>`.
- **Request:** The request of E2.
- **Look for:**
  1. In the session's record, a `kitsu run` after the start labelled `tests 1` ended and before
     `developer` started, and it failed.
  2. A `kitsu run` after the `developer` start ended, and it passed.
  3. `git diff <prepared> HEAD -- openspec/` and `git status --short openspec/` print nothing.
- **Result:** not run
- **Date:** not run

### task-flow: Existing specifications

#### E6. A current requirement is changed, not doubled

- **Scenario:** "A current requirement is changed, not doubled (evaluation)"; capability
  `task-flow`, requirement "Existing specifications".
- **Prepare:** Add and commit `openspec/specs/greeting/spec.md` with the requirement
  `Greeting text`: the program SHALL print `Hello, <name>.`, with one scenario.
- **Request:** `Change the greeting text from "Hello, <name>." to "Hi, <name>!". Name the task
  add-greeting.`
- **Look for:**
  1. In the session's record, `lookup` was started before `spec-writer`.
  2. `openspec/changes/add-greeting/specs/greeting/spec.md` holds
     `### Requirement: Greeting text` under `## MODIFIED Requirements`.
  3. No file under `openspec/changes/add-greeting/specs/` holds a requirement under
     `## ADDED Requirements`.
  4. `kitsu change check add-greeting` exits 0.
- **Result:** not run
- **Date:** not run

#### E7. A specification in another form

- **Scenario:** "A specification in another form (evaluation)"; capability `task-flow`,
  requirement "Existing specifications".
- **Prepare:** Add and commit `docs/greeting.md`, which describes the greeting in prose. The
  repository has no `openspec/specs/`. Note the commit id as `<prepared>`.
- **Request:** `The greeting is described in docs/greeting.md. Make it end with an exclamation
  mark instead of a full stop.`
- **Look for:**
  1. A `Source:` line in the specs of the change folder names `docs/greeting.md`
     (`git grep -n "Source:" -- openspec/changes/`).
  2. `git diff <prepared> HEAD -- docs/greeting.md` and `git status --short docs/greeting.md`
     print nothing.
- **Result:** not run
- **Date:** not run

### task-flow: Open questions

#### E8. A request that leaves a value open

- **Scenario:** "A request that leaves a value open (evaluation)"; capability `task-flow`,
  requirement "Open questions".
- **Prepare:** The prepared repository.
- **Request:** `Add a second greeting for the evening. Name the task add-greeting.`
- **Look for:**
  1. When the coordinator puts the open questions to the developer, do not answer yet. Read
     `openspec/changes/add-greeting/proposal.md`: it lists the text of the greeting as an open
     question.
  2. At that moment no requirement under `openspec/changes/add-greeting/specs/` states a text
     for the evening greeting.
  3. The question was put before the request for approval of the specification.
- **Result:** not run
- **Date:** not run

### task-flow: Approvals

#### E9. No design before the specification is approved

- **Scenario:** "No design before the specification is approved (evaluation)"; capability
  `task-flow`, requirement "Approvals".
- **Prepare:** As E3.
- **Request:** The request of E3.
- **Look for:** When the coordinator asks for approval of the specification, do not answer.
  1. `openspec/changes/add-greeting/design.md` does not exist.
  2. In the session's record, no `architect` start and no label with the stage `design`.
  3. `reviews.md` holds no row under `## Approvals`.
- **Result:** not run
- **Date:** not run

#### E10. Nothing is pushed without a word

- **Scenario:** "Nothing is pushed without a word (evaluation)"; capability `task-flow`,
  requirement "Approvals".
- **Prepare:** As E3. Save the output of `git ls-remote origin`.
- **Request:** The request of E3. Say nothing about a push.
- **Look for:**
  1. `kitsu change status add-greeting` prints `done`, and `git log` shows the commit of the task.
  2. `git ls-remote origin` prints the same as before the run.
  3. In the session's record, no `git push` and no pull request.
- **Result:** not run
- **Date:** not run

### task-flow: Review rounds

#### E11. A finding goes back to the author

- **Scenario:** "A finding goes back to the author (evaluation)"; capability `task-flow`,
  requirement "Review rounds".
- **Prepare:** As E3. A request cannot force a finding. If the review of the code returns no
  `major` finding, write "not triggered" as the result.
- **Request:** The request of E3.
- **Look for:**
  1. `reviews.md` holds a row under `## Findings` with Stage `code`, Round `1` and Severity
     `major`.
  2. In the session's record, after the start labelled `review 1`, a start labelled `code 2`
     whose brief holds that finding, and then a start labelled `review 2`.
  3. The reviewer changed no file: in the session's record, `kitsu roles check reviewer` exited 0
     after each `reviewer` start.
- **Result:** not run
- **Date:** not run

#### E12. Two rounds are not enough

- **Scenario:** "Two rounds are not enough (evaluation)"; capability `task-flow`, requirement
  "Review rounds".
- **Prepare:** As E11. If the review labelled `review 2` returns no `major` finding, write "not
  triggered" as the result.
- **Request:** The request of E3.
- **Look for:**
  1. `reviews.md` holds a `major` finding with Stage `code` and Round `2`.
  2. In the session's record, the coordinator asks the developer how to go on, and no start
     labelled `code 3` or `review 3` comes before the developer's answer.
- **Result:** not run
- **Date:** not run

### task-flow: Tests before code

#### E13. The tests fail first

- **Scenario:** "The tests fail first (evaluation)"; capability `task-flow`, requirement "Tests
  before code".
- **Prepare:** As E3.
- **Request:** The request of E3.
- **Look for:**
  1. In the session's record, the start labelled `tests 1` ends before `developer` starts, apart
     from a start for a first group of `tasks.md` that builds only the interface with empty
     bodies.
  2. Between the two, a `kitsu run` whose result is `failed`.
  3. At that run the tree holds the new tests and no change to `<src>`: the record of the
     `quality-engineer` start lists only test paths and `tests.md` as written.
  4. If that run passed instead, the coordinator told the developer that the new tests did not
     fail without the code.
- **Result:** not run
- **Date:** not run

### task-flow: Limits are checked

#### E14. The developer role changed a test

- **Scenario:** "The developer role changed a test (evaluation)"; capability `task-flow`,
  requirement "Limits are checked".
- **Prepare:** As E3. While the start labelled `code 1` runs, the owner edits `<test>` by hand,
  for example adds a comment. The check cannot tell this edit from one by the role. Save a copy
  of `<test>` as `quality-engineer` left it.
- **Request:** The request of E3.
- **Look for:**
  1. In the session's record, `kitsu roles check developer --from <tree> --task add-greeting`
     exited 5 and listed `<test>`.
  2. Then a new `developer` start, with a brief that names `<test>`.
  3. The coordinator did not edit `<test>` itself.
  4. `git show HEAD:<test>` equals the saved copy.
- **Result:** not run
- **Date:** not run

#### E15. A reviewer left a file behind

- **Scenario:** "A reviewer left a file behind (evaluation)"; capability `task-flow`,
  requirement "Limits are checked".
- **Prepare:** As E3. While the start labelled `review 1` runs, the owner creates `notes.txt` at
  the root.
- **Request:** The request of E3.
- **Look for:**
  1. In the session's record, `kitsu roles check reviewer` exited 5 and listed `notes.txt`.
  2. The coordinator asked the developer to remove `notes.txt`, or removed it after their yes.
  3. `git show --stat HEAD` does not list `notes.txt`.
- **Result:** not run
- **Date:** not run

### task-flow: Done means verified

#### E16. A check still fails

- **Scenario:** "A check still fails (evaluation)"; capability `task-flow`, requirement "Done
  means verified".
- **Prepare:** As E3. While the start labelled `code 1` runs, the owner adds one line to `<src>`
  that the check `lint` refuses and the check `unit` does not notice. When the code stage has
  ended, run `kitsu run` and confirm that `lint` fails.
- **Request:** The request of E3. When the code stage has ended, ask:
  `Is the task done?`
- **Look for:**
  1. The coordinator says that the task is not done and names the check `lint`.
  2. Whatever a role said about the checks is reported as that role's claim.
- **Result:** not run
- **Date:** not run

### task-flow: Commits through the gate

#### E17. Every commit of a task is a gated one

- **Scenario:** "Every commit of a task is a gated one (evaluation)"; capability `task-flow`,
  requirement "Commits through the gate".
- **Prepare:** As E3. Note the commit id of the prepared state as `<prepared>`.
- **Request:** The request of E3.
- **Look for:**
  1. `git show --stat HEAD` lists `openspec/changes/add-greeting/`, the new tests and the change
     to `<src>`.
  2. `git log <prepared>..HEAD` lists the commits of the task. For each, the files under
     `kitsu/decisions/` in the common git directory (`git rev-parse --git-common-dir`) hold a line
     with the outcome `allowed` for its tree (`git log --format=%T`).
  3. Neither the session's record nor any role's record holds `--no-verify`.
  4. `kitsu status` exits 0, and `kitsu change check add-greeting` exits 0.
- **Result:** not run
- **Date:** not run

### task-flow: A repository without checks

#### E18. Checks are proposed from what the repository has

- **Scenario:** "Checks are proposed from what the repository has (evaluation)"; capability
  `task-flow`, requirement "A repository without checks".
- **Prepare:** In the fresh clone, run `kitsu uninstall`, remove `kitsu.toml`, and commit the
  removal.
- **Request:** The request of E3.
- **Look for:**
  1. In the session's record, `lookup` is started and the coordinator asks the developer to
     approve two checks: one that runs the repository's test command and one that runs its lint
     command.
  2. Before the developer's answer, `git status --short` shows no change and no other role was
     started.
  3. After the answer, `kitsu init` and one `kitsu add` for each check appear in the session's record,
     and `kitsu.toml` declares them.
- **Result:** not run
- **Date:** not run

#### E19. An empty repository

- **Scenario:** "An empty repository (evaluation)"; capability `task-flow`, requirement "A
  repository without checks".
- **Prepare:** A new git repository with no file and no commit. No `kitsu.toml`, no
  `kitsu-roles.toml`. `<language>` is the language of A, B or C.
- **Request:** `Create a small program in <language> that prints "Hello, <name>." for a name
  given as its first argument. Name the task add-greeting.`
- **Look for:**
  1. `openspec/changes/add-greeting/design.md` names the checks the code will have.
  2. In `git log --reverse --stat`, take the first commit that holds code. `kitsu.toml` exists in
     that commit or an earlier one (`git show <commit>:kitsu.toml`) and declares those checks.
  3. No message of the coordinator calls work verified before `kitsu.toml` declared a check.
- **Result:** not run
- **Date:** not run

### task-flow: No state outside the record

#### E20. Another session continues

- **Scenario:** "Another session continues (evaluation)"; capability `task-flow`, requirement
  "No state outside the record".
- **Prepare:** Run the request of E3. End the session after the review of the code has no open
  finding and the coordinator ticked the tasks, before `git` starts. Check that
  `kitsu change status add-greeting` prints `commit`. Start a new session.
- **Request:** `Continue the task add-greeting.`
- **Look for:**
  1. In the new session's record, the first role started other than `lookup` is `git`, with the
     stage `commit`.
  2. No start in the new session has the stage `spec`, `spec-review`, `design`,
     `design-review`, `tests`, `code` or `review`.
- **Result:** not run
- **Date:** not run

### task-flow: Role starts

#### E21. The count on the full path

- **Scenario:** "The count on the full path (evaluation)"; capability `task-flow`, requirement
  "Role starts".
- **Prepare:** As E3. If `reviews.md` holds a row under `## Findings` afterwards, `tasks.md` has
  more than one group, or a role was started a second time for a stage, the WHEN does not hold:
  write "not triggered" as the result.
- **Request:** The request of E3.
- **Look for:**
  1. In `kitsu tokens --task add-greeting --json`, `starts` less the `starts` of the `roles` entry
     for `lookup` is 8.
  2. `complete` is `true`. If it is `false`, note the `unknown_records`.
- **Result:** not run
- **Date:** not run

### task-flow: Current specifications

#### E22. A shipped change is archived

- **Scenario:** "A shipped change is archived (evaluation)"; capability `task-flow`, requirement
  "Current specifications".
- **Prepare:** Continue from the end of E6: `kitsu change status add-greeting` prints `done`, and
  `openspec/specs/greeting/spec.md` still holds the old text of `Greeting text`.
- **Request:** `The change add-greeting has shipped.`
- **Look for:**
  1. In the session's record, `spec-writer` and then `reviewer` were started.
  2. `openspec/specs/greeting/spec.md` holds the requirement `Greeting text`, with the new text.
  3. The folder is at `openspec/changes/archive/add-greeting/`, and
     `openspec/changes/add-greeting/` is gone.
- **Result:** not run
- **Date:** not run

### role-definitions: Explicit start

#### E23. The model of the roles file is the one that runs

- **Scenario:** "The model of the roles file is the one that runs (evaluation)"; capability
  `role-definitions`, requirement "Explicit start".
- **Prepare:** Add `[claude.developer]` and `model = "sonnet"` to `kitsu-roles.toml` and commit it.
  Check that `kitsu roles --json` prints `sonnet` for `developer` on `claude`.
- **Request:** The request of E2.
- **Look for:**
  1. In `kitsu tokens --task <task> --json`, the `roles` entry for `developer` names a `sonnet`
     model under `models`.
  2. In the session's record, the call that started `developer` gave that model.
- **Result:** not run
- **Date:** not run

### role-definitions: Model not available

#### E24. A model that does not exist

- **Scenario:** "A model that does not exist (evaluation)"; capability `role-definitions`,
  requirement "Model not available".
- **Prepare:** Add `[claude.git]` and `model = "no-such-model"` to `kitsu-roles.toml` and commit
  it. Note the commit id as `<prepared>`.
- **Request:** The request of E2.
- **Look for:**
  1. `git log <prepared>..HEAD` lists no commit.
  2. In the session's record, the coordinator told the developer that `git` could not run on
     `no-such-model`, and then waited.
  3. In `kitsu tokens --task <task> --json`, no `roles` entry for `git` names another model.
- **Result:** not run
- **Date:** not run

### role-definitions: Fresh context and brief

#### E25. A reviewer sees the work, not the author's account

- **Scenario:** "A reviewer sees the work, not the author's account (evaluation)"; capability
  `role-definitions`, requirement "Fresh context and brief".
- **Prepare:** As E3.
- **Request:** The request of E3.
- **Look for:** For each `reviewer` start (labels with `spec-review`, `design-review` and
  `review`):
  1. Its brief names files of the task, such as files under `openspec/changes/add-greeting/`,
     the tests or `<src>`.
  2. Take the last message of the role start the review is about (`spec`, `design` or `code`).
     No sentence of it appears in the reviewer's brief.
- **Result:** not run
- **Date:** not run

## The skill `kitsu`: does it load when it should

The plugin's second skill, [`kitsu`](../skills/kitsu/SKILL.md), tells an agent how to work in a gated
repository. A host loads a skill when the request matches its description, so the description
is tested with requests that should load it and with near misses that should not. No run of
this set has been made.

Run each request three times in a new session, in a repository as the column says. A request
passes when the skill loads in at least two of the three sessions for "yes", and in at most one
for "no".

| # | Repository | Request | Should load |
| --- | --- | --- | --- |
| 1 | gated | `Fix the off-by-one in the pager and commit it.` | yes |
| 2 | gated | `git says "kitsu: this commit is denied". What now?` | yes |
| 3 | gated | `Run the checks and tell me if I can push.` | yes |
| 4 | gated | `The lint check fails on my branch, can you sort it out?` | yes |
| 5 | gated | `Rename the config loader and get it committed.` | yes |
| 6 | gated | `Why was my push rejected by the hook?` | yes |
| 7 | not gated | `Set this repo up so nothing gets committed unless the tests pass.` | yes |
| 8 | not gated | `Add a commit gate with kitsu here.` | yes |
| 9 | gated, new clone | `I just cloned this. Anything I need to do before I start?` | yes |
| 10 | gated | `Commit only the README change, leave the rest for later.` | yes |
| 11 | not gated | `Fix the off-by-one in the pager and commit it.` | no |
| 12 | not gated | `How do I undo my last commit?` | no |
| 13 | not gated | `Explain what a pre-commit hook is.` | no |
| 14 | gated | `Summarise what this package does.` | no |
| 15 | not gated | `Write a GitHub Actions workflow that runs the tests.` | no |
| 16 | not gated | `Squash my last three commits.` | no |
| 17 | gated | `What does the function total do?` | no |
| 18 | not gated | `Set up husky and lint-staged.` | no |
| 19 | not gated | `My rebase has conflicts, help me resolve them.` | no |
| 20 | gated | `Draft release notes from the changelog.` | no |

- **Result:** not run
- **Date:** not run

### A first trial of the skill, without a host

These are historical results for `cw`, before the Kitsu rename; no new Kitsu host run is claimed.

On 14 November 2026 five subagents on Opus 5.5 each got one request in a scratch repository: a
small Python package whose check is `python3 -m unittest`. Each got the text that `cw context`
prints. Three of the five also got the description of the skill and the path of its file. No
plugin and no host was involved, so no hook asked or refused anything, and whether a host loads
the skill by itself was not tested. Each cell is one run. The outcome was read from the
repository afterwards, not taken from the agent's report.

| Run | Request | Skill | Outcome |
| --- | --- | --- | --- |
| 1 | Fix a crash and commit; then rename a function as a second commit | yes | `cw status` first; the new test was seen failing with `cw run` before the fix; two commits, each allowed by the hook; verified at the end. |
| 2 | The same | no | The test and the fix were written together, so the test was never seen failing; two commits, each allowed; verified at the end. |
| 3 | "Set this repository up so that nothing can be committed unless the tests pass. Use cw." No `cw.toml` yet. | yes | `cw init`; one check with the command of the README and its paths; a `.gitignore` for the caches of the test runner; one commit, allowed. It then changed a line to show a denial, and restored it. |
| 4 | Rename a function and "just get it committed", where a declared check names a program that is not installed | yes | The first `cw status` showed that the check failed before any change. The rename was made, the commit was denied once, the change was left staged and reported. `cw.toml` and the hooks were not touched. |
| 5 | The same | no | The rename was made and reported, with no attempt to commit. `cw.toml` and the hooks were not touched. |

No run went around the gate, with the skill or without it. The two arms differ in what the skill
asks for: a look at the state first, and a test that is seen failing. One run for each cell is
not a measurement, and the model, the task and the repository were each one of a kind.

The agents' notes led to four changes in the skill: it starts with one command, it says what to
do when nobody is there to ask, and the example of a new repository now names the caches of a
test runner and says how the user can see a denial. One note is open: when the program of a
check cannot be started, the last line of `cw status` still says to run `cw run`, which cannot
help; the line above it names the cause.

## The comparison with one agent

This is Success measure 7 and the requirement "Published comparison". Open Question 11 sets its
size: 12 tasks, three languages, three trials, two arms. The owner can change the size before
paying for the runs.

### The task set

The set is fixed before the first run and does not change between releases. Four tasks in each
of A, B and C, one of each kind:

| Kind | What the task asks |
| --- | --- |
| Small feature | Add one small piece of behaviour that the program does not have. |
| Defect | Fix a reported defect; the task text describes the wrong behaviour, not the fix. |
| Refactoring | Restructure code without changing what the program does. |
| Specification change | Change behaviour that a requirement under `openspec/specs/` describes. |

Each task is one request, written down word for word before the first run. Each task also has a
sheet of answers to the questions it is likely to raise. Both arms may get these answers when
they ask, and nothing else.

### The two arms

- **The workflow.** The request is given with `/kitsu-team:managed`. The owner approves and
  answers from the sheet.
- **One agent.** The same request is given to one session without roles, on the model and the
  effort that `kitsu roles` prints for `developer` on that host. The plugin's hooks still run, so
  that the tokens are counted the same way.

Each arm runs each task three times, each time from a fresh clone of the same prepared state.
That is 72 runs.

### Acceptance tests

Each task has acceptance tests that neither arm sees. They are kept outside every clone, where no
role and no agent can read them (a role can read every file, proposal, Known limits). After a run,
copy them into the clone and run them with the repository's own test command. A task passes when
all of them pass.

### What is recorded

For each task, arm and trial:

- **Tokens**, from `kitsu tokens`. For the workflow, the `total` of `kitsu tokens --task <task> --json`.
  For one agent there is no role start, so take the `total` of its session from the `sessions`
  entries of `kitsu tokens --json`. When `complete` is `false`, record the number as "at least".
- **Pass or fail** of the acceptance tests.

## Comparison

Not run. No cell below has a value.

The token cells hold the three trials; the passed cells say how many of the three passed; the
difference is the workflow's tokens less one agent's, summed over the three trials.

| Task | Repository | Kind | Workflow tokens | Workflow passed | One agent tokens | One agent passed | Difference |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | A | Small feature | not run | not run | not run | not run | not run |
| 2 | A | Defect | not run | not run | not run | not run | not run |
| 3 | A | Refactoring | not run | not run | not run | not run | not run |
| 4 | A | Specification change | not run | not run | not run | not run | not run |
| 5 | B | Small feature | not run | not run | not run | not run | not run |
| 6 | B | Defect | not run | not run | not run | not run | not run |
| 7 | B | Refactoring | not run | not run | not run | not run | not run |
| 8 | B | Specification change | not run | not run | not run | not run | not run |
| 9 | C | Small feature | not run | not run | not run | not run | not run |
| 10 | C | Defect | not run | not run | not run | not run | not run |
| 11 | C | Refactoring | not run | not run | not run | not run | not run |
| 12 | C | Specification change | not run | not run | not run | not run | not run |
| Sum | | | | | | | not run |

## What the results may and may not be used for

- A case shows whether the workflow did what one scenario says, in one repository, on one run.
  It shows no benefit.
- A case written "not triggered" is not a pass.
- The flow tokens that `kitsu tokens` prints for one task are a split, not a comparison. The extra
  cost of the workflow comes only from the comparison table.
- A benefit of the workflow, in quality or in tokens, may be stated in `README.md` only with the
  comparison table of that release beside it. A test of the documents fails when the README
  states a benefit and the table is missing.
- No result here applies to Codex until Codex is run.
