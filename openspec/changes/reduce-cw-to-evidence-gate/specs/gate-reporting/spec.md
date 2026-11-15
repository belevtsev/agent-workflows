# gate-reporting

## Purpose

Tell a person or an agent, before they try, whether a commit would be allowed
and what is missing, and give each repository a local count of what the gate
allowed and denied, so that its benefit can be measured.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` of the `check-evidence` specification, with the hooks
  installed and the gate enforcing.
- "Checked state" means `kitsu run` has just ended with both checks `passed` and
  nothing has changed since.

## ADDED Requirements

### Requirement: Status

`kitsu status` SHALL print, for the working tree's snapshot, each declared check with its state: `satisfied`, `not run`, `failed` or `timed out`. It SHALL exit 0 when the snapshot is verified and 5 when it is not, and SHALL change nothing.

Source: review finding H9 (the only way to learn whether a commit would pass today is `kitsu task verify`, which needs a task).

#### Scenario: Everything is satisfied

- **WHEN** `kitsu status` runs in the checked state
- **THEN** it prints `unit` and `lint` as `satisfied` and exits 0

#### Scenario: Something is missing

- **WHEN** `src/app.txt` holds `broken`, `kitsu run` has ended, and `kitsu status` runs
- **THEN** it prints `unit` as `failed` with the path of the log, prints `lint` as `satisfied`, prints the command `kitsu run`, and exits 5

#### Scenario: No checks are declared

- **WHEN** `kitsu.toml` holds only `version = 1` and `kitsu status` runs
- **THEN** it exits 0 and says that no checks are declared, so every commit is allowed

#### Scenario: The index file is not rewritten

- **WHEN** `kitsu status` runs in the checked state
- **THEN** the index file of the repository has the same bytes and the same modification time as before

### Requirement: Staged content

`kitsu status` SHALL say whether the index holds the same tree as the snapshot. When it does not, `kitsu status` SHALL list up to 20 of the paths that differ, give the count of the rest, and say whether the tree of the index is itself verified.

Source: review finding H1; `publish-gate`, Commit rule (a commit is judged by the tree it records).

#### Scenario: Part of the work is staged

- **WHEN** `README.md` and `src/app.txt` are edited, `kitsu run` exits 0, `git add README.md` runs, and `kitsu status` runs
- **THEN** it exits 0, lists `src/app.txt` as not staged, and says that a commit of the index alone would be denied

### Requirement: Gate state

`kitsu status` SHALL warn when git does not run the hooks of `kitsu` in this clone: a hook file is missing or does not call `kitsu hook`, or `core.hooksPath` names a directory whose hooks do not. The warning SHALL name `kitsu init`, and the first line of `kitsu status` SHALL then not say that a commit would be denied. `kitsu status --json` SHALL hold `hooks`, which is `installed` or `missing`. The exit status SHALL NOT depend on it.

Source: production plan, Independent challenge, finding 7 (a clone without hooks, or a changed `core.hooksPath`, turned the gate off with nothing reported); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding U6.

#### Scenario: A clone without hooks

- **WHEN** `demo` is cloned in the checked state and `kitsu status` runs in the clone
- **THEN** it warns that the hooks are not installed and names `kitsu init`

#### Scenario: A hooks directory that does not call `kitsu`

- **WHEN** `git config core.hooksPath .githooks` is set after `kitsu init`, `.githooks/` holds no hook, and `kitsu status --json` runs
- **THEN** `hooks` is `missing`

#### Scenario: Status says first that nothing judges yet

- **WHEN** `demo`, in which no check has run, is cloned, and `kitsu status` runs in the clone
- **THEN** it exits 5, and the first line of its output is `not verified, but no hook of kitsu judges a commit in this clone yet`

### Requirement: Status as JSON

`kitsu status --json` SHALL print one JSON object with `tree`, `verified`, `index_tree`, `index_verified`, `mode` and `checks`. Each entry of `checks` SHALL hold `id`, `state` and, when a run decides the state, that run's `run_id`, `tree`, `outcome`, `exit_status`, `ended_at` and `log`. No field SHALL be removed or change its meaning while the declaration `version` is 1.

Source: proposal, What Changes (the plugin and scripts read the state from `kitsu`, not from files).

#### Scenario: The object for a failed check

- **WHEN** `src/app.txt` holds `broken`, `kitsu run` has ended, and `kitsu status --json` runs
- **THEN** `verified` is `false`, `mode` is `enforce`, and the entry with `id` `unit` has `state` `failed`, `outcome` `failed` and `exit_status` 3

### Requirement: Recorded decisions

Each hook decision SHALL be recorded with its time, the hook, the mode, the tree it judged, the outcome, and for a denial the state of each check that was not satisfied or the cause that prevented a decision.

Source: review finding H7; proposal, Success measure 5.

#### Scenario: A denial is recorded with its reasons

- **WHEN** a commit is denied because `unit` failed and `lint` was not run, and `kitsu stats --json` runs
- **THEN** it counts one denied `pre-commit` decision, with `unit` under `failed` and `lint` under `not_run`

### Requirement: Statistics

`kitsu stats` SHALL print, from the repository's own records for the last 30 days or for the period `--since` gives: runs per check by outcome; hook decisions by hook, mode, outcome and reason; and, per check, how often it passed after failing on the same checked content. `--json` SHALL print the same as one JSON object.

Source: review finding H7 (`docs/evidence.md:16-18, 158-172`; evals ran once per case with no baseline).

#### Scenario: A week of work

- **WHEN** over a week five commits were allowed, two were denied because `unit` had failed, one was denied because nothing had been run, and `unit` once passed on a rerun without any change to its checked content, and `kitsu stats --since 7d` runs
- **THEN** it reports 5 allowed and 3 denied `pre-commit` decisions, 2 of them for a failed check and 1 for checks not run, and 1 pass after failure for `unit`

#### Scenario: Observed denials are counted apart

- **WHEN** the gate was in observe mode for one commit that would have been denied, and `kitsu stats` runs
- **THEN** that decision is counted under observe mode and not among the enforced denials

### Requirement: Exit statuses

`kitsu` SHALL exit 0 when it did what was asked and the answer is yes; 1 when it could not do it; 2 when the command line is wrong; 3 when a hook denies; 4 when `kitsu init` or `kitsu uninstall` leaves a hook of another tool for the user to edit; and 5 when a check did not pass or the content is not verified.

Source: `docs/operations.md` exit status table (the same numbers and meanings today).

#### Scenario: The statuses in one session

- **WHEN** `kitsu status` runs in the checked state, then `kitsu run --nosuchflag`, then `kitsu status` after `src/app.txt` is edited, then `git commit -a -m change`
- **THEN** the exit statuses are 0, 2 and 5, and `kitsu hook pre-commit` exits 3
