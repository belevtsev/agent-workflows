# Tasks: add-role-workflow

Requirement and scenario names refer to the four files under `specs/`.
Decisions, formats and building blocks refer to [design.md](design.md).
Success measures and Open Questions refer to [proposal.md](proposal.md).
Findings are in `reports/Review findings 2026-11-14.md`.

Rules for every task:

- **Tests.** A scenario without the mark (evaluation) becomes a Go test,
  written from the specification by another agent than the task's
  implementer. A scenario with the mark becomes an evaluation case: a written
  task for a model run, with what to look for afterwards. Evaluation cases are
  not run in CI.
- **Systems.** The Go tests pass on `ubuntu-latest`, `macos-latest` and
  `windows-latest`. Nothing was observed on macOS or Windows.
- **Owner.** A task that begins with the word Owner needs the owner: it spends
  money on model runs or publishes something.

Order: this change starts when the gate has its commands (the first change's
groups 3 to 7), because it uses the snapshot, the differing paths and the
record store.

## 1. Decisions

- [x] 1.1 Write into `proposal.md` that Open Questions 1 to 12 take their recommended answers, on whose word, and how each is undone. Size: S. Depends on: none. Check: each of the twelve questions names its answer and who gave it. Source: proposal, Open Questions; the owner's statement of 14 November 2026.

## 2. Roles

- [x] 2.1 Build `internal/roles` with the seven roles, their jobs, their write limits and their default models, and the commands `cw roles` and `cw roles --json`. Size: M. Depends on: none. Check: scenarios under "Role set" and "Default models". Source: `role-definitions`: Role set, Default models; design.md, Interfaces.
- [x] 2.2 Read `cw-roles.toml`: models and efforts per host and role, and the test paths. Refuse an unknown key, role or host with the file, the line and the rule. Size: S. Depends on: 2.1. Check: scenarios under "Roles file". Source: `role-definitions`: Roles file.
- [x] 2.3 Build `cw roles check`: compare the snapshot with a tree, by the kind of each change, against the limit of the role. Size: M. Depends on: 2.2. Check: scenarios under "Write limits". Source: `role-definitions`: Write limits; H6.
- [x] 2.4 Write the seven role texts and the coordinator's instructions under `internal/roles/text/`. Each role text says the job, what the role reads, what it may write and what it hands back. Size: L. Depends on: 2.1. Check: scenarios under "Role text names no language" and "Text budget"; `cw roles` prints each size. Source: `role-definitions`: Role text names no language, Text budget, Fresh context and brief, Model not available; `task-flow`; M24; H4.
- [x] 2.5 Generate the files Claude Code needs from the texts: one role file each, with model, effort and tools, and the skill that holds the coordinator's instructions. A test fails when a generated file differs from its text. Size: M. Depends on: 2.4. Check: scenarios under "One text per role" and "Roles without a writing tool", and scenario "No role can start a role". Source: `role-definitions`: One text per role, Roles without a writing tool, Explicit start; C1.
- [x] 2.6 Tests: the scenarios of `role-definitions`, 19 as Go tests and 3 as evaluation cases (22). Size: M. Depends on: 2.3, 2.5. Check: `go test -race ./...` passes, and the three evaluation cases are in `docs/evaluation.md`. Source: `specs/role-definitions/spec.md`.

## 3. Change record

- [x] 3.1 Build `internal/change`: read the six kinds of file of a change folder into values, with the line of each item. Size: M. Depends on: none. Check: `go test ./...` passes; scenarios under "Folder" at 3.2. Source: `change-record`: Folder; design.md, The change folder.
- [x] 3.2 Build `cw change status` with `--json`: the stage from the files alone, what is missing, and the content ids that say whether a review or an approval is of the current content. Size: M. Depends on: 3.1. Check: scenarios under "Folder", "Stage from the files" and "Reviewed content". Source: `change-record`: Folder, Stage from the files, Reviewed content; H2, H9; production plan, Independent challenge, finding 9.
- [x] 3.3 Build `cw change check`: the rules for specifications, tasks, the test table, the design and the review records, each failure with its file, line and rule. Size: L. Depends on: 3.1. Check: scenarios under "Specification rules", "Task rules", "Test table", "Design rules", "Review records" and "Usable as a check". Source: `change-record`: Specification rules, Task rules, Test table, Design rules, Review records, Usable as a check.
- [x] 3.4 Make the two change folders of this repository pass `cw change check`. Size: S. Depends on: 3.3. Check: `cw change check` exits 0 in this repository. Source: Success measure 3.
- [x] 3.5 Tests: the scenarios of `change-record` (29). Size: M. Depends on: 3.2, 3.3. Check: `go test -race ./...` passes. Source: `specs/change-record/spec.md`.

## 4. Tokens

- [x] 4.1 Build the reader for a Claude Code transcript in `internal/tokens`: the four kinds of tokens of each answer, counted once, the model, and the label of the brief. Start from `_quarantine/internal/usagelog/claude.go` and `lines.go`. Size: M. Depends on: none. Check: `go test ./...` passes; scenarios under "Kinds of tokens" at 4.3. Source: `token-report`: Kinds of tokens, Task label; `_quarantine/internal/usagelog/`.
- [x] 4.2 Build `cw tokens record`: read the host's notice from standard input, store counts and no text under `cw/tokens/`, and store nothing where the root holds no `cw.toml`. Size: M. Depends on: 4.1. Check: scenarios under "Counts from the host's record" and "Local and without text". Source: `token-report`: Counts from the host's record, Local and without text; H5.
- [x] 4.3 Build `cw tokens` with `--task` and `--json`: by role, by stage and round, writing and flow tokens, the share, unlabelled starts, session tokens, and unknown counts that mark a sum as incomplete. Size: M. Depends on: 4.2. Check: scenarios under "Task label", "Kinds of tokens", "Flow tokens", "Session tokens" and "Unknown is not zero". Source: `token-report`: Task label, Kinds of tokens, Flow tokens, Session tokens, Unknown is not zero; M11.
- [x] 4.4 Add to the plugin the hooks that run `cw tokens record` when a role or a session ends. Size: S. Depends on: 4.2. Check: a test reads the plugin's hook file and finds one command for each of the two events. Source: `token-report`: Counts from the host's record; proposal, Impact.
- [x] 4.5 Build the reader for a Codex record of a session or a role, from `_quarantine/internal/usagelog/codex.go`, and report `unknown` where the record holds no count. Write into `proposal.md` under Open Question 12 what the Codex documentation says. Size: M. Depends on: 4.3. Check: scenario "A Codex session". Source: `token-report`: Both hosts; Open Question 12; M11.
- [x] 4.6 Tests: the scenarios of `token-report` (16). Size: M. Depends on: 4.3, 4.5. Check: `go test -race ./...` passes; the two scenarios under "Published comparison" are tests of the documents. Source: `specs/token-report/spec.md`.

## 5. Flow

- [x] 5.1 Write into the coordinator's instructions the two paths, the stages, the approvals, the review rounds, tests before code, the check of each role's limit, and when a task is done. Size: M. Depends on: 2.4, 3.2. Check: every requirement of `task-flow` is stated in the instructions, and the text stays within its budget. Source: `task-flow`: Entry, Two paths, Stages of the full path, Stages of the small path, Existing specifications, Open questions, Approvals, Review rounds, Tests before code, Limits are checked, Done means verified, Commits through the gate, A repository without checks, No state outside the record, Role starts, Current specifications; `role-definitions`: Text budget.
- [x] 5.2 Write `docs/evaluation.md`: the 25 evaluation cases of `task-flow` and `role-definitions`, each with its repository, its request and what to look for in the session's record and in the repository afterwards; and the task set of the comparison with one agent. Size: M. Depends on: 5.1. Check: each scenario marked (evaluation) has one case. Source: Success measures 1 and 7; `token-report`: Published comparison.
- [x] 5.3 Tests: the scenarios of `task-flow`, 1 as a Go test and 22 as evaluation cases (23). Size: S. Depends on: 5.2. Check: `go test -race ./...` passes, and the 22 cases are in `docs/evaluation.md`. Source: `specs/task-flow/spec.md`.

## 6. Documents and self-use

- [x] 6.1 Write `docs/roles.md`, `docs/workflow.md` and `docs/tokens.md`, and a section of `README.md` that says what the roles are, that their benefit is not measured, and what flow tokens do and do not mean. Size: M. Depends on: 4.3, 5.1. Check: `go test ./...` passes; the text claims no benefit. Source: proposal, Known limits; H6, H7.
- [x] 6.2 Add tests for Success measures 2, 4 and 6: the size limits of the Go code and of the texts. Size: S. Depends on: 2.4, 4.3. Check: the tests pass and fail in a trial with a limit lowered by one. Source: Success measures 2, 4 and 6.
- [x] 6.3 Declare this repository's own checks in `cw.toml`, with `cw change check` among them, and name those checks in the Rules table of both designs. Size: S. Depends on: 3.4. Check: `cw run` exits 0 in this repository, and a commit goes through the hooks. Source: Success measure 3; `change-record`: Usable as a check.

## 7. Evaluation

- [ ] 7.1 Owner: run the evaluation cases with a model in three repositories of three languages, and write the result into `docs/evaluation.md`. Size: L. Depends on: 5.2. Check: each case has a result and a date. Source: Success measure 1; Open Question 11.
- [ ] 7.2 Owner: run the comparison with one agent on the same tasks and publish the table. Size: L. Depends on: 7.1. Check: scenarios under "Published comparison". Source: Success measure 7; `token-report`: Published comparison; H7.
- [ ] 7.3 Owner: confirm on Codex how a role is started on a model, how `cw` learns that a role ended, and where tokens are recorded. Size: M. Depends on: 4.5. Check: `proposal.md` holds the answer under Open Question 12, with the document that gives it. Source: Open Question 12.

## Coverage

| Capability | Requirement | Built in | Tested in |
| --- | --- | --- | --- |
| `role-definitions` | Role set | 2.1 | 2.6 |
| | Default models | 2.1 | 2.6 |
| | Roles file | 2.2 | 2.6 |
| | Explicit start | 2.4, 2.5 | 2.6 |
| | Model not available | 2.4 | 2.6 |
| | Write limits | 2.3 | 2.6 |
| | Roles without a writing tool | 2.5 | 2.6 |
| | Fresh context and brief | 2.4 | 2.6 |
| | Role text names no language | 2.4 | 2.6 |
| | One text per role | 2.5 | 2.6 |
| | Text budget | 2.4 | 2.6 |
| `task-flow` | Entry | 5.1 | 5.3 |
| | Two paths | 5.1 | 5.3 |
| | Stages of the full path | 5.1 | 5.3 |
| | Stages of the small path | 5.1 | 5.3 |
| | Existing specifications | 5.1 | 5.3 |
| | Open questions | 5.1 | 5.3 |
| | Approvals | 5.1 | 5.3 |
| | Review rounds | 5.1 | 5.3 |
| | Tests before code | 5.1 | 5.3 |
| | Limits are checked | 2.3, 5.1 | 5.3 |
| | Done means verified | 5.1 | 5.3 |
| | Commits through the gate | 5.1 | 5.3 |
| | A repository without checks | 5.1 | 5.3 |
| | No state outside the record | 3.2, 5.1 | 5.3 |
| | Role starts | 5.1 | 5.3 |
| | Current specifications | 5.1 | 5.3 |
| `change-record` | Folder | 3.1, 3.2 | 3.5 |
| | Stage from the files | 3.2 | 3.5 |
| | Specification rules | 3.3 | 3.5 |
| | Task rules | 3.3 | 3.5 |
| | Test table | 3.3 | 3.5 |
| | Design rules | 3.3 | 3.5 |
| | Review records | 3.3 | 3.5 |
| | Reviewed content | 3.2 | 3.5 |
| | Usable as a check | 3.3 | 3.5 |
| `token-report` | Counts from the host's record | 4.2, 4.4 | 4.6 |
| | Task label | 4.1, 4.3 | 4.6 |
| | Kinds of tokens | 4.1, 4.3 | 4.6 |
| | Flow tokens | 4.3 | 4.6 |
| | Session tokens | 4.3 | 4.6 |
| | Unknown is not zero | 4.3 | 4.6 |
| | Local and without text | 4.2 | 4.6 |
| | Both hosts | 4.5 | 4.6 |
| | Published comparison | 5.2, 7.2 | 4.6 |

| Success measure | Tasks |
| --- | --- |
| 1. One sentence to a gated commit | 5.2, 7.1 |
| 2. Any language | 2.4, 6.2 |
| 3. Clarity | 3.4, 6.3 |
| 4. Size | 6.2 |
| 5. Role starts | 5.1, 7.1 |
| 6. Token report | 4.6 |
| 7. Measured benefit and extra tokens | 7.2 |

## Not in this list

- Roles on hosts other than Claude Code and Codex, advisers, prices and
  budgets, language packs (proposal, Non-goals).
- A program that merges requirement changes into the current specifications.
