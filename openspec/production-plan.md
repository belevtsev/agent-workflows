# Production plan: the evidence gate and the roles

Written on 14 November 2026. The owner asked for this plan, said that nobody
can guide the implementation, and wants something to test on return. This file
is the overview. The engineering content is in the two changes:

| Part | Proposal | Requirements | Design | Tasks |
| --- | --- | --- | --- | --- |
| 1. The gate | [proposal](changes/reduce-cw-to-evidence-gate/proposal.md) | 55, with 154 scenarios | [design](changes/reduce-cw-to-evidence-gate/design.md) | [64 tasks](changes/reduce-cw-to-evidence-gate/tasks.md) |
| 2. The roles | [proposal](changes/add-role-workflow/proposal.md) | 45, with 91 scenarios | [design](changes/add-role-workflow/design.md) | [27 tasks](changes/add-role-workflow/tasks.md) |

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
4. **The skill.** `skills/cw/` tells a coding agent how to work with the gate
   for any task, with seven examples that a test replays against `cw`.

What users report about other tools, and what `cw` did about each finding, is
in [docs/other-tools.md](../docs/other-tools.md). Three of those findings speak
against parts of this plan, and the page says so first.

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
- The size limit of the gate's code. It holds 5,997 of 6,000 lines after
  the guard learned to read a command as words. The next change to the gate
  needs a cut elsewhere, or the owner's word on the limit.
- Confirming the behaviour on macOS, on Windows and on Codex. The build
  machine is Linux, and CI does not run for a branch that is not pushed.

Tasks that stay open on 14 November 2026, by what they wait for:

| Waits for | Part 1 | Part 2 |
| --- | --- | --- |
| The owner's decision or account | 1.1, 1.2, 1.3, 1.4, 11.1, 11.3, 11.4, 12.1, 12.2 | 7.1, 7.2, 7.3 |
| A CI run on the three systems | 2.4, 3.4, 5.3, 5.4, 5.5, 6.9, 6.10, 6.11, 7.4, 8.4, 11.5 | |
| Windows, for one scenario of their check | 4.4, 4.7, 6.2 | |
| The owner's clone and CI | 10.4 | |
| Work that was not done | 3.5 (the git observations as a test), 10.1 (the hook-cost test), 11.2 (the release workflow) | |

The tests those CI tasks name exist and pass on Linux. 37 of the 64 tasks of
part 1 and 24 of the 27 of part 2 are ticked: each one's check passed here as
it is written.

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
| M9 | Review against other tools, and the usage skill | part 1: 8.5, 8.6 | The findings of the review each have an outcome; the skill's examples are replayed by a test; the guard reads a command as words; the token readers are compared with real records | `docs/other-tools.md`, `skills/cw/`, and part 10 of `docs/try-it.md` |

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
| 14 Nov 2026 | `publish-gate`: Hooks of other tools | `cw init` exits 4 only for a hook that does not call `cw hook <name>` yet; one scenario added. Part 1 now has 145 scenarios | A unit test showed that `cw init` exited 4 for ever in a repository whose shared hooks already called `cw`. The test of scenario "Another tool's hook still calls `cw`" expected that exit in its setup and was changed with the specification |
| 14 Nov 2026 | Design, Records | `pass_after_failure` compares runs of the same command; a base revision implies nothing for a check that has a decisive run of its own, however old; `--name-status`; a run without an end belongs to the period it started in | Two unit tests failed against the design (the code was fixed), and two sentences of the design did not say what the code does |
| 14 Nov 2026 | `agent-integration`, design: Plugin; proposal, Open Questions 6 and 11 | A tenth command, `cw guard`, holds the plugin's question before a tool call; Codex denies because it cannot ask | The hosts' documentation |
| 14 Nov 2026 | `check-declaration`: fixture, the meaning of "Refused" | "Writes nothing" now says what it covers: no new or changed file or directory in the working tree, the home directory or the git directory; objects git stores for a snapshot are not counted | Three scenario tests failed because a refused `cw run` and `cw status` left an empty `cw/` directory. The code was fixed; the wording was made exact |
| 14 Nov 2026 | `change-record`: new requirement Reviewed content; Stage from the files; fixture | Each review and each approval holds a content id and counts only for that content; a stage `commit` between `review` and `done`; the fixture is committed; six scenarios added | Challenge findings 8 and 9: an approval stayed valid after the approved file changed, and `done` was printed for a record nobody had committed |
| 14 Nov 2026 | `role-definitions`: Write limits | `cw roles check` warns, as `cw status` does, in a clone whose hooks git does not run; one scenario added. Part 2 now has 45 requirements and 89 scenarios | Challenge finding 7 |
| 14 Nov 2026 | `change-record`: Reviewed content | The `code` id covers every path outside `openspec/changes/`, not outside the one folder. A review of the code also counts for the last commit that changed the folder only when `HEAD` holds the folder as the working tree does. Two scenarios reworded: an edit of the code counts as a change after the review while the record is not committed, and as later work once it is | Found while building: as first written, the rule and its scenario "The code changes after its review" contradicted each other, and the id of one folder made a second task's record stale |
| 14 Nov 2026 | `role-definitions`: Role text names no language; Text budget | The search also covers the files generated for a host; one scenario added for the sizes `cw roles` prints. Part 2 now has 90 scenarios | A scenario test failed: the header of each generated role file named a Go command, and a host gives that file to a model. The header was changed. The test agent also found that the sentence about the sizes had no scenario |
| 14 Nov 2026 | `change-record`: three sentences | "says that it has no scenario"; "the file `tests/app_test.txt`"; "the line where the broken rule has one" | The test agent found each of them open to two readings. More rules now name a line: a requirement no task names, a scenario without a row, a cycle |
| 14 Nov 2026 | `change-record`: fixture | The texts of the two tests are `greets_by_name` and `greets_without_name` | Found while trying the scenario "A test is renamed" by hand: `no_name_is_given` holds `name_is_given`, so the renamed test was still found |
| 14 Nov 2026 | Part 2 design, Tokens: the notice | `cw tokens record` reads a fifth field of the notice, `agent_type`, and stores the role it names for a start whose record gives no label. An unknown start without a label is a start of that role in the one task of its session; in every other case each task of its session is incomplete and names the record | Two scenario tests failed ("A record that cannot be read", "The report says that it is incomplete"): with an empty record the label was lost with the counts, so the start was listed in no task and the task stayed `complete`. The specification was right and the design had a gap; the code and the design were changed |
| 14 Nov 2026 | Part 2 design, Tokens: a record of Codex | A `token_usage_record` without a `response_id` is a running total and is not counted; a record that holds only running totals holds no count | The scenario test "A Codex session" failed: the code counted such a line as one more request, so every start was counted twice. The code was fixed and the sentence made exact |
| 14 Nov 2026 | Part 2 design, Tokens: three sentences | Without `--host` the host is `claude`; the Codex hook at the end of a session has a timeout of 3 because Codex allows 1 to 3 seconds; with an unknown start the sentence of the report says "N of M known tokens" | The test agent found the first two unstated. The third was found by hand: a share of an incomplete total was printed as if it were the share of the whole |
| 14 Nov 2026 | Part 2 design | A block of 69 lines that repeated the sections from "The change folder" to "Tokens" was taken out | A broken edit of mine on 14 Nov 2026 left it there; the writer of `docs/evaluation.md` found it. No requirement and no scenario changed |
| 14 Nov 2026 | Both designs, Rules | "Checked by" names the checks `test` and `record` of `cw.toml` in place of the word "review". The rule "Only `internal/gitx` starts git" now says how it holds: a program is started only by `internal/proc`, which only `internal/gitx` and `internal/runner` import | Task 6.3 of part 2. Two rules had no test; `internal/repocheck` now has one for each |
| 14 Nov 2026 | Part 1 design: Declaration, Clean-up, Gate state, Plugin, `--base`; proposal, Known limits | Six sentences now say what the code does: a pattern with any empty segment is refused; the clean-up goes by the time a file was last written and also removes decision files and index copies; a hook counts only when it can be executed; the Codex command for Windows prints no line when `cw` is missing; the base implies nothing for a check with an unreadable or future-dated record; a working tree with uncommitted changes in a submodule is refused | The writer of `docs/operations.md`, `docs/hooks.md` and `docs/evidence.md` compared the design with the code and listed seven differences. In each the code was right or stricter. The seventh, a limit of 1,000 characters that nothing enforced, got a test |
| 14 Nov 2026 | Part 2 design: Tokens, `cw change status`, Failure behaviour | A role that ended without a record is stored with unknown tokens; a damaged stored file makes the tasks of its session incomplete; `cw change status` also prints the number of broken rules; `cw roles text` does not read `cw-roles.toml`; `cw tokens record` does not read the notice outside a gated repository | The writer of `docs/roles.md`, `docs/workflow.md` and `docs/tokens.md` found five cases with a concrete failure and six differences between design and code. The code was changed in seven places and the design in four |
| 14 Nov 2026 | Part 1, task 10.3 | "Leave no complexity exemption" now says "for code that is not a test" | The limits hold for every function that is not a test. Test files keep one exemption, with its reason in `.golangci.yml`: a test function is as long as its cases. Without it five test functions exceed a limit |
| 14 Nov 2026 | `agent-integration`: Ask before skipping; The plugin never judges a commit or push; design, Plugin; proposal, Known limits | The guard reads a shell command as words: it asks before `--no-verify`, `-n` of `git commit`, a setting of `core.hooksPath`, and an edit of a hook file or of a record. The same text inside an argument is no reason to ask. Five scenarios added. "Never judges" now says what it always meant: the plugin does not decide about content; it reads a command only to ask before a skip | Review of other tools, row G-2: the old rule asked before `grep -- --no-verify docs/hooks.md` and missed `git commit -n`. `internal/shellwords` is a new package of 151 lines; the package of that name that the first review criticised decided the gate, and this one decides nothing |
| 14 Nov 2026 | `agent-integration`: new requirement Usage skill | The plugin ships the skill `cw`; a test replays its examples and allows only commands `cw help` lists; no skill holds a `cw` version; four scenarios added. Part 1 now has 55 requirements, 154 scenarios and 64 tasks | The owner's request of 14 November 2026. The advice to record a tool version in the skill was rejected against the requirement "Instruction text comes from `cw`" |
| 14 Nov 2026 | `task-flow`: Two paths | For a change of a few lines with a plain cause the coordinator says that the task needs no roles; one scenario and the case E26 added. Part 2 now has 91 scenarios. The texts of `spec-writer` and `architect` say to write no more than the behaviour needs | Review of other tools, rows R-1 and R-2 |
| 14 Nov 2026 | Part 2 design, Tokens: a record of Codex, a record of Claude Code | Codex writes running totals in `token_count` events; the thread is counted once from them, and a total that falls or starts above its own request is unknown. The line `token_usage_record` that the first reader expected exists in no real record | The readers were run on real records: 170 of Claude Code 2.1, where the reader was right, and 16 of codex-cli 0.141 and 0.142, where it found no count |

## Progress

| Milestone | State | Commit | Evidence |
| --- | --- | --- | --- |
| M1 | done | `4fff8ad` | The two change folders and this plan are on the branch. No pull request: publishing is the owner's. |
| M2 | done | `cb59433` | Local, Linux: `go build`, `go vet`, `go test -race` pass; 2,216 files hold the same blob under `_quarantine/`; `cw version` exits 0 and `cw nosuch` exits 2; `claude plugin validate .` passes. Task 2.4 is written but not ticked: its check is a CI run. |
| M3 | done, Linux only | `df3f3b5`, tests in the commit after it | Local, Linux: the 62 scenarios of tasks 5.3 to 5.5 each have a test, written from the specifications by agents that did not read the code. 58 pass; 3 need Windows and are skipped; 1 waits ten minutes and runs only with `CW_SLOW_TESTS=1`; it ran once, on 14 Nov 2026, and passed in 600 seconds. Three tests failed first (a refused command left an empty `cw/` directory); the code was fixed. Tasks 3.x to 5.x are not ticked where their check names the three systems. |
| M4 | done, Linux only | `9211f5b`, tests in `ba569f7` | Local, Linux, by hand in a demo repository: an unchecked commit is denied and a checked one recorded; a commit that leaves a file out names the file; a push of a commit made around the hook is denied; a push of another branch says where to run `cw run`; a tag on a commit the remote has, a new branch at that commit and a deletion go through; observe mode prints one line; a missing `cw` and an unreadable store deny; `cw uninstall` removes the hooks and the store. Scenario tests since then, local, Linux: commit gate 27, push gate 17, reporting 16 (15 pass; the offline one is skipped because this machine does not allow a network namespace), platform 8 (3 run; 5 need Windows and are only compiled). Unit tests for `decl`, `store`, `stats`, `gitx`, `evidence` and `gate` pass. |
| M5 | done, not run on a host | `ba569f7` | Local, Linux: the 12 scenarios of `agent-integration` each have a test and pass; they run the hook commands of both hook files with `sh -c` and a notice on standard input. `claude plugin validate .` passes. Nothing was run inside Claude Code or Codex. |
| M6 | done, not run with a model | `ba569f7`, `90aee13` | Local, Linux: `cw roles`, `cw roles check`, `cw roles text`, `cw change status` and `cw change check` exist. The 19 scenarios of `role-definitions` that need no model and the 29 of `change-record` each have a test, written from the specifications by agents that did not read the code; all pass. `cw change check` exits 0 on the two change folders of this repository, after it found 18 requirements that no task named. No role was ever started: the texts were not tried with a model. |
| M7 | done; compared with real records in M9 | `90aee13`, `501ebd8` | Local, Linux: the 16 scenarios of `token-report` each have a test and pass, 14 against records built to the shape the reader expects and 2 as tests of the documents. Three scenario tests failed first and were settled against the specification (see the table above). A record built to the table of the specification gives 655,000 tokens, of which 225,000 (34%) are workflow tokens; no text of a brief is stored. No record that Claude Code or Codex wrote was read. |
| M8 | done, except what needs the owner | `501ebd8` | Local, Linux: `README.md`, eleven documents under `docs/` with an index, and `docs/try-it.md`, whose parts 1 to 9 were run by hand with the output shown. The quick start is executed by a test. Tests of the documents: links and anchors resolve, the index names every document, no benefit is stated while the comparison table holds no numbers. `cw.toml` declares five checks. In a second clone of this branch at `501ebd8`, made in a scratch directory, `cw init` installed the hooks, `cw run` passed all five checks, a commit was allowed, a commit of changed content was denied and named the five checks, and a commit with a failing test was denied and named the check `test`. The owner's clone was not touched: it has no hooks of this `cw` and no records. The two writers of the reference documents compared the program with the design and reported eleven cases with a concrete failure and thirteen differences; all were settled in the code or in the design, and six got a test under `scenarios/messages`. Not done: the release workflow (task 11.2), the hook-cost test (10.1), the git observations as a test (3.5), and everything that needs CI, macOS, Windows, Codex or a model. |
| M9 | done, not run on a host | the commit after `e50e20b` | Local, Linux. **Review:** four research agents read the sources listed in `docs/other-tools.md`; its author did not open them again. Each of 48 findings has an outcome: changed, already so, rejected or deferred. **Skill:** seven examples, each replayed by `TestSkillExamplesAreReplayed` against the `cw` under test; the replay found four places where a draft showed other output than `cw` prints. **Guard:** table tests and four scenario tests through both hook files. **Tokens:** `cw tokens record` and a separate script gave the same totals for 170 real records of Claude Code and for 13 real rollouts of Codex. **Model trial:** five runs of a subagent on Opus 5.5 in scratch repositories, three with the skill and two without, read back from the repositories: no run went around the gate; one run for each cell is not a measurement. `gofmt`, `go vet` and `golangci-lint` for three systems, `govulncheck`, `go test -race -count=1 ./...` in 27 packages, the specification and task lints, and `claude plugin validate .` pass. Not done: a run of the plugin on Claude Code or Codex, the trigger test of the skill, CI. |
