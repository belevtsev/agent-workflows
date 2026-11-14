# Benchmarks

What `cw` costs: in time for every commit, and in tokens for an agent that works with the gate
or with the roles. Everything here was measured on 14 November 2026. Each section says where it
was measured and on which commit or CI run.

These are first measurements. Each table says how many runs stand behind it. None of them is the
comparison that [the evaluation](evaluation.md#the-comparison-with-one-agent) defines, and none
shows that the gate or the roles make code better.

## Contents

- [What a commit costs](#what-a-commit-costs)
  - [Where the time goes](#where-the-time-goes)
- [What cw costs against the size of a repository](#what-cw-costs-against-the-size-of-a-repository)
- [What an agent spends](#what-an-agent-spends)
  - [The gate](#the-gate)
  - [The roles on a defect](#the-roles-on-a-defect)
  - [The roles on new behaviour](#the-roles-on-new-behaviour)
  - [What the runs say about cw](#what-the-runs-say-about-cw)
  - [What they do not show](#what-they-do-not-show)
- [How to repeat them](#how-to-repeat-them)

## What a commit costs

A commit pays for one decision of the pre-commit hook: the hook file starts `cw`, and `cw` looks
the content up in its records. It executes no check.

The test `scenarios/cost` measures that decision in CI on the three systems: the hook file started
with `sh` as git starts it, from the start of `sh` to its exit. Each number is the median of 15
decisions after one warm-up. The first number is for a copy of this repository (about 2,500
files, 18 MiB), the second for a generated repository of 30,000 files and 68 MiB.

| CI run | Linux, amd64 | macOS, arm64 | Windows, amd64 |
| --- | --- | --- | --- |
| Run 4 of pull request 219 | 14 ms, 26 ms | 68 ms, 66 ms | 195 ms, 200 ms |
| Run 8 | 18 ms, 28 ms | 35 ms, 50 ms | 164 ms, 179 ms |
| Run 15 | 15 ms, 26 ms | 66 ms, 71 ms | 197 ms, 209 ms |
| Run 19 | 12 ms, 23 ms | 53 ms, 66 ms | 150 ms, 167 ms |
| Run 43638791541, started by hand | 11 ms, 20 ms | 38 ms, 48 ms | 169 ms, 178 ms |
| Run 43641527063 | 15 ms, 27 ms | 113 ms, 64 ms | 203 ms, 215 ms |
| Run 43642931023 | 13 ms, 33 ms | 77 ms, 78 ms | 167 ms, 183 ms |

- The limits are 150 ms for this repository and 500 ms for the generated one. They apply to the
  median, and hold on Linux and macOS in every run. On macOS the first number went from 35 to
  113 ms between runs, and one decision of run 43641527063 took 294 ms.
- On Windows the decision took 150 to 203 ms on this repository. No limit is set there; the test
  reports the number only.
- The runners are shared machines. The spread between runs on one system is as large as the
  difference between the two repositories.

### Where the time goes

A decision starts seven programs: `sh` for the hook file, `cw`, and five times `git` (the
directories of the repository, the tree of the index, `cw.toml` in that tree in two calls, and
the tree of the last commit). Run 43642931023 also measured two parts alone, as the median of 15:
a hook file that only exits, started with `sh` the same way, and `cw version`, started without
`sh`.

| On this repository | Linux | macOS | Windows |
| --- | --- | --- | --- |
| A hook file that only exits | 0.6 ms | 6.3 ms | 19.9 ms |
| `cw version` | 2.4 ms | 10.1 ms | 10.5 ms |
| The whole decision | 13.0 ms | 76.8 ms | 167.0 ms |
| The rest: what the decision adds to the two starts | 10.0 ms | 60.4 ms | 136.6 ms |

- The start of `sh` and of `cw` is about a fifth of the decision on every system: 23 in a hundred
  on Linux, 21 on macOS and 18 on Windows, where it is 30 of 167 ms.
- The rest is the five `git` processes and the reading of the records. It was not split
  further. Divided by five it is 2 ms for each `git` on Linux, 12 ms on macOS and 27 ms on
  Windows.
- So the number of `git` processes that a decision starts is likely what the Windows cost
  depends on most. That was not measured, and the number was not reduced.

## What cw costs against the size of a repository

One machine: Linux, AMD Ryzen 9 3900X, git 2.53, `cw` built from commit `37f9d5c`, the
repositories on a file system in memory.
Each repository is generated: that many files of 1 KiB, one trivial check (`git --version`).
Each number is the median of 11 calls after one warm-up, in milliseconds.

| Files | `git status` | `cw status` | Commit, no hooks | Commit, gated | `cw run`, nothing to run | `cw run`, one check |
| --- | --- | --- | --- | --- | --- | --- |
| 100 | 4 | 20 | 5 | 14 | 27 | 37 |
| 1,000 | 10 | 26 | 6 | 17 | 58 | 75 |
| 10,000 | 59 | 97 | 14 | 38 | 245 | 345 |
| 30,000 | 70 | 140 | 24 | 61 | 345 | 503 |
| 100,000 | 117 | 297 | 62 | 143 | 699 | 1,058 |

- **Commit, gated** less **Commit, no hooks** is what the gate adds to a commit: 9 ms at 100
  files, 24 ms at 10,000 and 81 ms at 100,000.
- **`cw run`, one check** is the whole cost of recording one run of a check that itself takes
  about 2 ms: `cw` takes a snapshot of the working tree before and after the check. At 10,000
  files that is a third of a second, at 100,000 files one second. A check that takes a minute
  hides it; a check that takes a tenth of a second does not.
- The numbers are from one machine and one run of the script. While it ran, one agent session of
  the next section ran on the same machine, which has twelve cores. On a disk, and on macOS or
  Windows, the numbers will be higher; that was not measured.

## What an agent spends

Claude Code 2.1.311, started by a script with `claude -p`, in a new scratch repository of a few
files for each run. The session ran on `opus`, which `claude-opus-5-5` answered. `cw` and the
plugin were those of commit `37f9d5c`. No person took part, and one run stands behind each line.

Two tasks, each a request of a few sentences:

- **`duration`**, Python, new behaviour: add a function `parse_duration(text)` that turns a text
  such as `1h30m` or `1d2h3m4s` into seconds, and raises `ValueError` for anything else.
- **`slug`**, Go, a defect: `Slugify("Hello,  World")` returns `hello---world`, and the comment of
  the function says that a run of other characters becomes one hyphen.

Four arms:

- **Plain.** One agent in a repository without `cw` and without the plugin. The request ends
  with "Write the tests it needs, make them pass, and commit the change."
- **Gate.** The same request in a repository with `cw init`, its checks declared, and the plugin
  loaded.
- **Roles.** The gated repository with a `cw-roles.toml`, and the request given to the skill
  `task`, without that last sentence. Whenever the session stopped before the task was committed, the script answered with
  one fixed text: approved, take the option you recommend, and record it as my approval.
- **Roles, asked.** As Roles, with one sentence more: take it through the roles, also if the
  change is small.

| Task | Arm | Tokens | Of them output | Cost | Time | Role starts | Hidden tests |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `duration` | Plain | 177,169 | 2,512 | $0.17 | 27 s | | pass |
| `duration` | Gate | 143,383 | 2,230 | $0.16 | 23 s | | pass |
| `duration` | Roles | 6,950,768 | 272,558 | $9.84 | 42 min | 16 | pass |
| `slug` | Plain | 141,104 | 2,151 | $0.15 | 24 s | | pass |
| `slug` | Gate | 105,817 | 1,587 | $0.14 | 20 s | | pass |
| `slug` | Roles | 283,000 | 2,346 | $0.23 | 29 s | 0 | pass |
| `slug` | Roles, asked | 683,341 | 13,535 | $0.72 | 2 min 19 s | 4 | pass |

- **Tokens** are all four kinds the host counts: input, cache write, cache read and output.
  Between 87 and 93 in a hundred are cache reads, because the host sends the conversation so far
  with every step.
- **Cost** is what the host reported for the session (`total_cost_usd`), in dollars at its list
  prices of that day. It weighs the four kinds, so it compares two arms more fairly than the
  count of tokens does.
- **Time** is the time the sessions ran. The time a person needs to read a question and answer
  it is not in it.
- **Hidden tests** are tests of the task that no arm saw, in
  [`docs/benchmarks/acceptance/`](benchmarks/acceptance/): nine tests for `duration`, ten cases
  for `slug`. They ran on what each arm committed.

### The gate

- In both tasks the agent in the gated repository ran `cw run` after its change and committed
  through the hook. The request did not name `cw`; the text at the start of the session did.
- The gated sessions did not cost more than the plain ones: $0.16 against $0.17, and $0.14
  against $0.15. One run of each does not show that the gate saves anything. It shows no cost of
  the gate in tokens that one run can see.

### The roles on a defect

- Given the defect, the coordinator advised against the workflow ("This fix probably doesn't
  need the workflow"). Told to take the option it recommends, it wrote the failing test and the
  fix itself, ran `cw run` and committed through the hook. No role started.
- Asked for the roles, it took the small path: `quality-engineer`, `developer`, `reviewer` and
  `git`, with `cw roles check` after each. The reviewer had no finding. That run took $0.72 and
  2 min 19 s, where the plain agent took $0.15 and 24 s: 4.7 times the cost.
- `cw tokens` gives 68,428 of the 683,341 tokens to the writing of the test and the fix, and
  523,790 to the coordinator's own session.
- The fix adds four lines to `slug.go` and removes one in both runs with the skill `task`. In
  the two others it adds seven and removes two.

### The roles on new behaviour

- The coordinator took the full path: 16 starts of roles in 42 minutes. `spec-writer` started 4
  times, `reviewer` 5 times, `architect` 3 times, `developer` twice, `quality-engineer` and
  `git` once. The run took $9.84, where the plain agent took $0.17 and 27 seconds: 59 times the
  cost.
- It wrote 55 lines of code, 62 tests in 315 lines, and a record of 1,023 lines: a
  specification of 429 lines, a design of 292, tasks of 113, a proposal, a list of the tests and
  the reviews. The plain agent wrote 27 lines of code and 5 tests in 43 lines.
- `spec-writer` put twelve questions that the request leaves open, such as spaces, signs,
  upper-case units, a value of zero and an input that is not a text. The script answered "take
  the option you recommend". The plain agent settled the same points without a question.
- The results differ in one of those points. For an input that is not a text, such as `None`,
  the result of the roles raises `TypeError`: `spec-writer` recommended it and said that it
  departs from the words of the request. The plain result raises `ValueError`, as the request
  says. The hidden tests give a text only, so they do not see the difference.
- The reviews of the specification and the design recorded eleven findings, two of them major.
  One: "no upper limit on numbers" contradicts the limit of 4,300 digits that Python sets when a
  text becomes an int. The finding changed the specification, not what the code does: the plain
  result and the result of the roles both return the value for 4,300 digits and raise
  `ValueError` for 4,301. The review of the code had no finding.
- `cw tokens` gives 1,358,469 of the 6,950,768 tokens to the first round of each writing stage.
  2,612,892 are the coordinator's own session, 1,340,770 the reviews, 22,364 the commit by `git`,
  and 1,616,273 every later round of `spec-writer`, `architect` and `developer`. `cw tokens`
  counts a later round as rework whatever it was for: the second round of `spec-writer` wrote
  the developer's answers in, and the second round of `developer` wrote the function, after a
  first round that wrote its empty form for the tests. The three starts of `architect` took
  2,182,552.
- Both results pass the nine hidden tests. Nobody read the code of either, and whether the 62
  tests and the record are worth the difference was not judged.

### What the runs say about cw

- **The token count.** Six sessions ran with the plugin: the five above with the gate or the
  roles, and one first trial, which ran on `claude-haiku-5-5`. In each, the total of `cw tokens` equalled the total the host
  reported: 85,336, 143,383, 105,817, 283,000, 683,341 and 6,950,768 tokens. In the two sessions
  that used two models, the tokens of `git` equalled the host's total for `claude-sonnet-5-5`.
- **The models.** Each of the 20 starts of roles was answered by the model the coordinator named
  on the call: 18 times `claude-opus-5-5` for `opus`, twice `claude-sonnet-5-5` for `sonnet`.
  `lookup` was not started in any session.
- **The limits of the roles.** The coordinator ran `cw roles check` after 15 of the 20 starts,
  and each time the role had stayed within its limit. On the full path it left the check out
  after the five starts of `reviewer`, which may write nothing.
- **The guard.** In the first trial the agent was asked to run `git commit --no-verify`. With
  nobody to ask, the host refused the command and gave the reason of the guard.

### What they do not show

- One run stands behind each line. How far two runs of the same arm differ was not measured.
- Both tasks are as small as a task gets, in repositories of a few files. How the cost of the
  roles grows with the size of a task was not measured.
- A script approved everything. A developer who answers the twelve questions, or who stops a
  third round of a design, changes both the cost and the result.
- This is not the comparison of [the evaluation](evaluation.md#the-comparison-with-one-agent):
  that one needs real tasks in three repositories and a judgement of the result.
- Nothing was run on Codex.

## How to repeat them

The scripts are in [`docs/benchmarks/`](benchmarks/). Each one says at its top what it needs and
what it writes.

**The cost of a commit.** In a checkout of this repository, with Go:

```sh
CW_COST=1 go test -count=1 -v ./scenarios/cost/
```

It prints the median, the smallest and the largest decision for both repositories, and the two
parts measured alone. CI runs it on the three systems in the job `Native Go`.

**The cost against the size of a repository.** With git, Python 3 and a `cw` program:

```sh
python3 docs/benchmarks/timings.py "$(command -v cw)" /tmp/cw-timings /tmp/cw-timings.json
```

It generates the five repositories under the second argument, which must be an empty directory,
and writes the medians to the third. It takes a few minutes. Remove the directory afterwards.

**The tokens of an agent.** With Claude Code logged in, a `cw` program and a checkout of this
repository as the plugin:

```sh
python3 docs/benchmarks/tokens.py "$(command -v cw)" "$PWD" roles slug /tmp/cw-tokens
```

The arms are `plain`, `gate`, `roles` and `roles-asked`; the tasks are `duration` and `slug`. One
call runs one arm on one task in a new repository under the last argument and prints one line.
The numbers above were made with an earlier form of this script: the same steps, with the paths
of one machine written into it.
It spends tokens of the account that `claude` is logged in to: the table above says how many
each arm took here. The hidden tests of a task run on what an arm committed:

```sh
sh docs/benchmarks/accept.sh slug /tmp/cw-tokens/slug-roles/repo
```
