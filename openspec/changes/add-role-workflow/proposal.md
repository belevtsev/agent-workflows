# Proposal: add-role-workflow

Current interface names below use Gatecub. Dated observations describe the former `cw` tool, before this rename.

Status: draft of 14 November 2026, written from the owner's two statements of
that day (see Sources). Nothing in it was run with a model.

Later that day the owner asked for the implementation to go on without
guidance. Open Questions 1 to 12 therefore take their recommended answers.
The implementer chose them, not the owner; each says below how it is settled,
and the [design](design.md) is written to them. The owner can change any of
them on return.

This change builds on [reduce-cw-to-evidence-gate](../reduce-cw-to-evidence-gate/proposal.md)
and cannot start before that change has its plugin (its tasks of group 8).

## Why

These are historical results for `cw`, before the Gatecub rename; no new Gatecub host run is claimed.

The owner wants one tool that takes a task from a specification to clean,
tested code, in any language and any codebase, with named roles and the right
model for each role, and that says how many tokens the workflow itself costs.

The first change gives the hard part of that promise: no commit without a
passing run of the repository's own checks on exactly that content. It set the
roles aside as an optional layer. This change brings them back as the second
part of the same tool, and writes them anew, because the roles this repository
has today cannot be handed to other people as they are:

- **They assume Go.** The starter plan and the language pack are Go only, and
  the starter plan cannot run the default flow (H4).
- **Independence is a sentence in a prompt.** The test engineer works in the
  same checkout as the implementer, and nothing checks what either changed
  (H6).
- **A task is a lock.** A killed run blocks the next one (H2), and a finished
  task denies the next edit (H9). `cw task` has 27 command files and
  `internal/taskflow` 12,907 lines.
- **The text is long.** The orchestration skill and its references are
  141,780 characters in 25 files (M24 names the session text).
- **The cost is unknown and the benefit unmeasured.** A lite task takes 4 to
  13 role starts and a strict one 9 to 27, with no baseline (H7). The budget
  stop does nothing under Codex (M11).

## What the developer gets

In a repository of any language that holds `gatecub.toml`, inside Claude Code or
Codex, the developer writes one sentence, for example "add CSV export of
orders". Then:

1. The coordinator, which is the developer's own session, names the task
   `add-order-csv-export` and says which path it takes and why.
2. `lookup` finds the specifications and the code that exist. `spec-writer`
   writes `openspec/changes/add-order-csv-export/` with the proposal and the
   requirements. The developer answers the open questions, and `spec-writer`
   writes the answers in. `reviewer` reviews it. The developer approves.
3. `architect` writes `design.md` (the decision, the building blocks, the
   interfaces, the rules) and `tasks.md`. `reviewer` reviews it. The developer
   approves.
4. `quality-engineer` writes a test for every scenario and lists them in
   `tests.md`. It is the only role that changes or removes an existing test.
   `gatecub run` is expected to fail here.
5. `developer` writes the code, one group of tasks per start.
   `gatecub roles check developer` then shows that it changed no existing test.
6. `gatecub run` passes. `reviewer` reviews the code and the tests against the
   specification and the design. Every finding and what was done about it is
   in `reviews.md`. When no finding is open, the coordinator ticks the tasks.
7. `gatecub change check` passes. `git` commits; the gate allows it because the
   tree is verified. A push or a pull request waits for the developer's word.
8. `gatecub tokens --task add-order-csv-export` prints the tokens of every role and
   how much of the total the workflow itself used.

A task whose intended behaviour is already clear and does not change, such as
a defect, a refactoring or a change of tests only, takes the small path:
tests, the fix, a review and a commit, with no folder.

## The roles and their models

| Role | Job | Writes | Claude Code | Codex |
| --- | --- | --- | --- | --- |
| `lookup` | Finds and quotes files, symbols and documents. Draws no conclusion. | nothing | `haiku`, `high` | `gpt-6-luna`, `high` |
| `spec-writer` | Writes the requirements and scenarios from the request and its sources. | `proposal.md`, `specs/` | `opus`, `high` | `gpt-6.1-sol`, `high` |
| `architect` | Decides the design: building blocks, interfaces, rules, tasks. | `design.md`, `tasks.md` | `opus`, `xhigh` | `gpt-6.1-sol`, `max` |
| `quality-engineer` | Writes the tests from the specification, before the code. | test paths, `tests.md` | `opus`, `high` | `gpt-6.1-sol`, `high` |
| `developer` | The principal developer. Writes the code to the design. | everything else | `opus`, `high` | `gpt-6.1-sol`, `high` |
| `reviewer` | Reviews the specification, the design, and the code with its tests. | nothing | `opus`, `xhigh` | `gpt-6.1-sol`, `max` |
| `git` | Commits, pushes and opens pull requests. | nothing in the working tree | `sonnet`, `medium` | `gpt-6-luna`, `high` |

The owner named five of them. `spec-writer` and `reviewer` are added because
the flow needs someone who writes the specification and someone who judges
work without having written it (Open Question 3).

The models are the ones this repository's own policy files give these jobs
today (Sources), except that `reviewer` runs at the effort of `architect`: a
review does not run at a lower effort than the work it reviews. A repository
changes any cell with one line in `gatecub-roles.toml`. The coordinator runs on the model of the developer's session.

## What Changes

- **New:** seven roles, each one text, with its job, what it reads, what it
  may write and what it hands back. The files a host needs are generated from
  that text.
- **New:** `gatecub roles` prints the table above as it applies in the repository.
  `gatecub-roles.toml`, an optional committed file, changes a model or an effort
  and names the test paths.
- **New:** `gatecub roles check <role> --from <tree>` says whether a role changed
  only what it may. It uses the snapshot and the differing paths of the core.
- **New:** the change folder is the whole record of a task. `gatecub change status`
  derives the stage from its files, and `gatecub change check` checks its rules:
  requirements, tasks, tests per scenario, design, review findings. A
  repository can declare `gatecub change check` as a check in `gatecub.toml`.
- **New:** two paths, full and small, with fixed stages. Tests come before
  code. The developer approves the specification and the design.
- **New:** `gatecub tokens` reports the tokens of a task by role, from the host's
  own record of the session, and splits them into writing tokens and flow
  tokens. Each release publishes the tokens of the same tasks with and without
  the workflow.
- **Changed:** the answer to Open Question 1 of the first change. The roles
  are not an optional layer somewhere else; they are the second part of this
  tool, in this repository and this plugin. The gate still needs no plugin
  and no role.
- **Not changed:** every verdict of the first change's
  [keep-and-cut list](../reduce-cw-to-evidence-gate/keep-and-cut.md). Today's
  workflow stays on its branch until this one passes its specification.

## Capabilities

### New Capabilities

- `role-definitions`: the seven roles, their models, their limits, and the
  commands that show and check them.
- `task-flow`: the two paths, the stages, the approvals and the rules between
  roles.
- `change-record`: the folder that records a task, and the commands that read
  and check it.
- `token-report`: the token count by role and the share the workflow used.

### Modified Capabilities

None. This change adds commands and plugin files and changes no requirement of
the first change.

## Impact

- **Code:** three new packages, for the roles, the change record and the token
  report. The token report starts from four files of today's
  `internal/usagelog` (`claude.go`, `codex.go`, `lines.go`, `session.go`,
  1,223 lines), without prices, budgets and history.
- **Plugin:** seven role texts, the coordinator's instructions, the host files
  generated from them, and hooks that tell `gatecub tokens record` when a role or a
  session has ended.
- **Documents:** the roles, the flow, the record and the token report; a page
  with the published comparison.
- **Tests:** the commands are tested without a model, on the three systems.
  What a model does is tested by an evaluation that the owner runs before a
  release, because CI has no model.
- **First change:** its Open Question 1 gets a new answer, and its
  keep-and-cut list a note. No task of it changes.

## Non-goals

- A `gatecub` that calls a model itself. The roles run inside the developer's
  coding agent.
- Hosts other than Claude Code and Codex in the first version. The gate works
  with any of them.
- Advisers: questions sent to models of other vendors (H5).
- Money: prices, budgets and a stop on spending (M11). `gatecub tokens` counts
  tokens.
- Language packs and domain skills. None ships with the roles.
- Roles that run at the same time, several tasks at once, and worktrees.
- A program that proves the code follows the design. A rule is checked by
  program only when the repository declares a check for it.
- Merging requirement changes into the current specifications by program. The
  spec writer does it and the reviewer checks it.
- A sandbox for a role.

## Known limits

- **The flow is instructions to a model.** `gatecub` checks afterwards what can be
  checked: the paths a role changed, the rules of the record, and the checks
  on the content. It cannot force the order of stages inside a session.
- **Approvals are written down by the coordinator.** Nothing proves that the
  developer gave them, apart from the developer reading `reviews.md` and the
  host's own prompt before a push.
- **Tests are independent only as far as this goes:** they are written before
  the code exists, and the developer role can add tests but cannot change one.
  In a second round the quality engineer can see the code.
- **A role can read every file.** Only writing is checked, and only after the
  role has ended, because a shell command can write too.
- **The tool does not make a design good.** It makes the design written,
  reviewed and traceable to tests. Whether the result is better than one agent
  working alone is not measured yet (Success measure 7).
- **Token counts rest on fields the host does not document.** Claude Code
  documents where the transcripts are, not what a line holds. When `gatecub` cannot
  read a count it says `unknown`.
- **Flow tokens are a split, not a comparison.** One agent working alone would
  also search and commit, and each role re-reads files in its own context. The
  real extra cost comes only from the published comparison.
- **Nothing was checked on Codex.** How it starts a role on a given model and
  where it records tokens is taken from this repository's own files.
- **Model names are not checked.** Hosts change them. A wrong name fails when
  the role starts.
- **On Claude Code the developer's own settings can override every role's
  model.** The token report shows the model that ran.
- **On Windows the roles work as far as the hosts do** (Open Question 11 of
  the first change).

## Success measures

1. **One sentence to a gated commit.** In three repositories in three
   languages, the full path ends with the stage `done` and a verified commit.
   The owner runs this evaluation before a release and publishes the result.
2. **Any language.** A test finds no name of a language, build tool, test
   framework or package manager in the role texts and the coordinator's
   instructions.
3. **Clarity.** `gatecub change check` passes on every change folder of this
   repository, the two that exist today included.
4. **Size.** This change adds at most 3,500 lines of production Go; the core
   keeps its limit of 6,000. No role text exceeds 8,000 characters, the
   coordinator's instructions 12,000, and all of them together 60,000. Today's
   texts are 150,080 characters.
5. **Role starts.** When `tasks.md` has one group and no role is started a
   second time for a stage, the full path starts at most eight roles other
   than `lookup`, and the small path at most four.
6. **Token report.** On recorded sessions kept as test data, `gatecub tokens`
   prints the known counts on the three systems.
7. **Measured benefit and extra tokens.** Before the README claims a benefit:
   at least 12 tasks in three languages, three trials each, run once with the
   workflow and once by one agent on the developer role's model. Published for
   both: how many passed the same hidden acceptance tests, and the tokens.

## Review findings this change answers

| Finding | Answer |
| --- | --- |
| H4 | No starter plan and no language pack; commands come from `gatecub.toml` (`role-definitions`: Role text names no language). |
| H6 | Tests before code, and a check of the paths each role changed (`task-flow`: Tests before code, Limits are checked; `role-definitions`: Write limits). |
| H7 | A limit on role starts, a token report, and a comparison with one agent (`task-flow`: Role starts; `token-report`). |
| H2, H9 | No task state outside the folder and git (`task-flow`: No state outside the record). |
| H5 | No adviser, and no text in the token records (`token-report`: Local and without text). |
| M11 | Tokens only, and `unknown` is never shown as zero (`token-report`: Unknown is not zero). |
| M24 | A limit on the size of the role texts (`role-definitions`: Text budget). |

## Sources

These are historical results for `cw`, before the Gatecub rename; no new Gatecub host run is claimed.

- The owner's statements of 14 November 2026: the tool must bring clarity and
  better code quality in any language and codebase; implement a task from
  scratch by creating a specification or working with existing ones; write
  code and tests; deliver clean architecture from reusable, well tested
  building blocks; be open source and used in production; have roles such as
  lookup, git, architect, quality engineer and principal developer, with the
  appropriate model for each; and count the tokens used extra because of the
  flow.
- This repository at `ce9b122`:
  - `agents/*.md`: seven role files with `model`, `effort` and
    `disallowedTools: Skill, Agent, Workflow`.
  - `skills/task-orchestration/model-policy.claude.yaml` and
    `model-policy.yaml`: the models and efforts in the table above.
  - `skills/task-orchestration/references/hosts.md`: how each host starts a
    role with a model; "Never silently move these roles to your own, an
    expensive or any other model".
  - `skills/task-orchestration/references/contracts/`: the flow, the review
    stages, the severities `blocker` and `major`, and the task format.
  - `internal/usagelog/`: reads a Claude Code transcript with its `subagents`
    folder, or a Codex rollout, and reports tokens per agent.
  - Sizes: `wc -c` of the skill and its references, 141,780 in 25 files, and
    of `agents/*.md`, 8,300; `wc -l` of `internal/taskflow` without tests,
    12,907.
- `reports/Review findings 2026-11-14.md`: H2, H4, H5, H6, H7, H9, M11, M24.
- Claude Code documentation, read on 14 November 2026:
  - `code.claude.com/docs/en/plugins/components.md`: a plugin's role file
    supports `model`, `effort`, `tools`, `disallowedTools` and `isolation`,
    and ignores `hooks` and `permissionMode`.
  - `code.claude.com/docs/en/sub-agents.md`: the model passed when a role is
    started wins over the role file; `CLAUDE_CODE_SUBAGENT_MODEL_FORCE`
    overrides both; a specifier in a tool rule removes the whole tool; a role
    can start roles of its own unless its tools forbid it.
  - `code.claude.com/docs/en/hooks.md`: every hook input has
    `transcript_path`; the one for the end of a role also has
    `agent_transcript_path`, `agent_id` and `agent_type`; no hook input holds
    a token total for a whole run.
  - `code.claude.com/docs/en/claude-directory.md`: where the transcripts are.
    It does not describe the fields of a line.
  - `code.claude.com/docs/en/monitoring-usage.md`: the metric
    `claude_code.token.usage` is the documented count for a whole run. It
    needs a telemetry collector.
- Not read: any Codex documentation.

## Open Questions

Each has a recommendation. On 14 November 2026 the implementer took every
recommendation, because the owner asked for the work to go on alone; none of
them is the owner's decision yet.

1. **Where do the roles run?** Recommendation: inside the developer's coding
   agent, Claude Code or Codex. `gatecub` calls no model and needs no key. The
   other option, a `gatecub` with its own model calls, is a different and much
   larger product. Settled by: the owner.
2. **Where do the roles live?** Recommendation: in this repository and this
   plugin, built after the core. This changes the answer to Open Question 1 of
   the first change. Settled by: the owner.
3. **Which roles?** Recommendation: the seven of the table. Without
   `reviewer`, somebody reviews their own work; without `spec-writer`, the
   architect writes the requirements it then designs for. Settled by: the
   owner.
4. **Which model for each role?** Recommendation: the table, which is today's
   policy. Whether `architect` should run on the host's strongest model is
   settled by Success measure 7, not by opinion.
5. **Is the OpenSpec folder the one format the tool writes?** Recommendation:
   yes. Specifications in any other form are read as sources and left as they
   are. Settled by: the owner.
6. **Which steps wait for the developer?** Recommendation: the specification,
   the design, and every push or pull request. Settled by: the owner.
7. **May the developer role write tests?** Recommendation: it may add a test
   file and may not change or remove one. Settled by: the owner.
8. **Where do the token counts come from?** Recommendation: the transcripts
   the host keeps, read when the host says a role or a session has ended. The
   documented alternative needs a telemetry collector on every machine.
   Settled by: a test on recorded sessions of each host.
9. **What counts as extra?** Recommendation: two numbers. Per task, the flow
   tokens: everything except the first round of the four roles that write.
   Per release, the comparison with one agent on the same tasks. Settled by:
   the owner.
10. **Are the limits right?** 3,500 lines of Go, 8,000 and 12,000 characters
    of text, eight and four role starts. They are proposals of this draft.
    Settled by: the owner, then by the first implementation.
11. **How large is the evaluation?** Recommendation: 12 tasks, three
    languages, three trials, two arms. Settled by: the owner, who pays for the
    runs.
12. **What does Codex allow?** Starting a role on a given model and effort,
    telling `gatecub` when a role has ended, and recording tokens per role.
    Recommendation: support Codex in the first version only where its
    documentation and a test confirm each of the three; report `unknown`
    elsewhere. Settled by: Codex documentation and a test.

### Answers taken on 14 November 2026

The implementer gave every answer below, alone, because the owner asked for
the work to go on without them. None is the owner's decision. Each can be
undone as the last column says.

| Question | Answer taken | How it is undone |
| --- | --- | --- |
| 1 | The roles run inside Claude Code or Codex; `gatecub` calls no model. | Nothing to undo: no model call was built. |
| 2 | The roles live in this repository and this plugin. | Move `agents/`, `skills/task/`, `internal/roles/`, `internal/change/` and `internal/tokens/` to another repository; the gate does not import them. |
| 3 | The seven roles of the table. | Change the list in `internal/roles/roles.go` and the texts under `internal/roles/text/`; the tests name what else must follow. |
| 4 | The models of the table. | Per repository in `gatecub-roles.toml`; for everyone in `internal/roles/roles.go`. |
| 5 | The OpenSpec folder is the one format written. | Replace `internal/change`; nothing else reads the folder. |
| 6 | The specification, the design, and every push or pull request wait for the developer. | Edit the coordinator's instructions, `internal/roles/text/coordinator.md`. |
| 7 | The developer role may add a test and may not change or remove one. | Change `developerLimit` in `internal/roles/roles.go`. |
| 8 | The counts come from the host's own records, read when the host says that a role or the session has ended. | Remove the two hook entries; `gatecub tokens` then reports nothing. |
| 9 | Two numbers: the flow tokens per task, and the comparison with one agent per release. | Change the split in `internal/tokens/report.go`. |
| 10 | The limits as proposed. | Change the numbers in the size tests. |
| 11 | 12 tasks, three languages, three trials, two arms. | Nothing was run; the owner sets the size before paying for runs. |
| 12 | Codex is supported where its documentation says so; see below. | Remove the Codex entries from `hooks/codex-hooks.json`. |

### What the Codex documentation says (question 12)

Read on 14 November 2026, on the page "Hooks" of the Codex documentation.
Nothing below was run on Codex.

- **A role has ended.** Codex documents a `SubagentStop` hook. Its input holds
  `session_id` (the parent session's id), `transcript_path`, `agent_id`,
  `agent_type` and `agent_transcript_path`; both paths may be null.
- **The session has ended.** Codex documents a `SessionEnd` hook for the main
  thread. It has a time limit of one to three seconds and its output steers
  nothing.
- **Tokens.** No input of these two hooks holds a count. A `PostModelRequest`
  hook holds `token_usage` "when available", and the page does not say what
  it contains. So `gatecub` reads the two transcripts the notices name.
- **The transcript.** The page says that its format "isn't a stable interface
  for hooks and may change over time", and does not describe a line. `gatecub`
  reads the lines the previous reader of this project knew. Where they hold
  no count for a request, the report says `unknown`.
- **A hook must not steer.** On `Stop` and `SubagentStop`, exit 2 and JSON on
  standard output continue the turn. `gatecub tokens record` prints nothing on
  standard output, and the hook command never exits 2.
- **Starting a role on a model.** Not on that page. It stays unconfirmed, and
  task 7.3 is the owner's.

