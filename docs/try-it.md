# Try it

This guide takes you through everything on this branch that can be tried by
hand. Parts 1 to 9 need no coding agent and take about twenty minutes. Every
command and every output in parts 1 to 9 was run on Linux, on 14 November
2026, with the program built from this branch. Part 10 was run in part, by a
script and not by a person; it says which steps.

Nothing here replaces a `kitsu` you already have installed, and nothing here
changes the repository you are reading.

## What you need

- Linux. It is the only system this was run on.
- git, with your name and e-mail set. The guide makes commits: when
  `git config user.name` or `git config user.email` prints nothing, set it
  first.
- Go 1.27 or newer to build the program.
- For part 9 only: `golangci-lint` on your `PATH`.

## 1. Build the program beside your installed one

In the root of this checkout:

```sh
mkdir -p "$HOME/kitsu-try"
go build -o "$HOME/kitsu-try/kitsu" ./cmd/kitsu
export PATH="$HOME/kitsu-try:$PATH"
kitsu version
```

Everything this guide creates is under `$HOME/kitsu-try`. If that folder exists
already, pick another, such as one from `mktemp -d`, and use it in place of
`$HOME/kitsu-try` below.

The first line must start with `kitsu v3.0.0`. If it does not, another `kitsu` is
still first on your `PATH`.

Stay in this shell for every part below. The git hooks call `kitsu` from `PATH`,
so a shell without the `export` line would call the program you had before.

## 2. The five commands, in a new repository

```sh
git init "$HOME/kitsu-try/demo"
cd "$HOME/kitsu-try/demo"
kitsu init
kitsu add no-bad -- test ! -e BAD
kitsu run
git add -A
git commit -m "first gated commit"
```

`kitsu init` prints:

```text
created kitsu.toml with no checks
installed the hooks pre-commit, pre-merge-commit, pre-push
the gate enforces: a commit or a push needs a passing run of every declared check
next: kitsu add <id> -- <program> [<argument>...], then kitsu run
```

`kitsu add` prints ``added the check `no-bad` to kitsu.toml``. `kitsu run` prints:

```text
== no-bad: test ! -e BAD
== no-bad: passed in 0s
every declared check has a passing run on this content
```

The commit is made. The check `no-bad` passes as long as no file named `BAD`
exists; it stands in for your tests.

## 3. A commit without a passing run is denied

```sh
echo note > notes.txt
git add -A
git commit -m "not verified"
```

The commit is not made, and git prints:

```text
kitsu: this commit is denied: its content has no recorded passing run of every declared check.
  no-bad: not run on this content
kitsu: to produce the missing evidence, run:
  kitsu run
```

`kitsu status` says the same without a commit, and exits 5:

```text
not verified: a commit of this content would be denied
  no-bad  not run on this content
index: holds the same content as the working tree
run `kitsu run` to produce the missing evidence
```

Now produce the evidence:

```sh
kitsu run
git commit -m "verified now"
```

This commit is made.

## 4. A check that fails, and a passing run that is used again

```sh
touch BAD
kitsu run
```

`kitsu run` exits 5 and names the log of the failed run:

```text
== no-bad: test ! -e BAD
== no-bad: failed with exit status 1
   log: <your home>/kitsu-try/demo/.git/kitsu/logs/<time>-<id>.log
not verified: 1 of 1 checks did not pass; fix what each log shows, then run `kitsu run` again
```

`git add -A` and `git commit -m "fails"` are denied:

```text
kitsu: this commit is denied: its content has no recorded passing run of every declared check.
  no-bad: failed with exit status 1; log: <your home>/kitsu-try/demo/.git/kitsu/logs/<time>-<id>.log
kitsu: a check failed or timed out; first fix what its log shows.
kitsu: to produce the missing evidence, run:
  kitsu run
```

Take the file away again:

```sh
git rm -f --cached BAD
mv BAD "$HOME/kitsu-try/BAD"
kitsu status
```

`kitsu status` prints `verified` without any new run: the content is again the
content of part 3, and that content has a passing run on record.

## 5. What the gate recorded

```sh
kitsu stats
```

```text
from 15 Oct 2026 17:51 to 14 Nov 2026 17:51 UTC, from the records of this repository

runs of each check, by outcome
  no-bad: passed 2, failed 1

passes after a failure on the same checked content
  none

hook decisions
  pre-commit, enforce mode: 2 allowed
  pre-commit, enforce mode: 2 denied
      failed: 1 (no-bad 1)
      not_run: 1 (no-bad 1)
```

The first line names the period it covers, and yours shows your own time.
These counts are what a later
measurement of the gate would be built on. They are counts and nothing more.

## 6. The roles, without a model

```sh
kitsu roles
kitsu roles text developer
```

`kitsu roles` prints the seven roles with their models on Claude Code and on
Codex, the size of each text and what each role may write. `kitsu roles text`
prints the text a model is given for one role. Read one or two of them: they
are the product, and no program can check their quality.

`kitsu roles check` compares the working tree with an earlier snapshot of it. Take
the snapshot, then act as a `reviewer` that left a file behind:

```sh
kitsu status --json
echo change > review-notes.txt
kitsu roles check reviewer --from <the value of "tree" in the JSON>
```

Take the first `"tree"` of the JSON, the one beside `"verified"`. A check
holds a `"tree"` of its own: the content its last run was on.

It exits 5:

```text
`reviewer` changed 1 of 1 paths outside its limit:
  review-notes.txt (added): `reviewer` may change nothing
`reviewer` writes nothing: ask the developer to remove these paths, or remove them after their yes; do not accept the work
```

Move the file out (`mv review-notes.txt "$HOME/kitsu-try/"`) and run the same
command again: it exits 0 and prints that the role stayed within its limit.

The command sees which paths changed. It cannot see who changed them.

## 7. The record of a task

This repository keeps two task records under `openspec/changes/`. In the root
of this checkout, in the same shell:

```sh
kitsu change status
kitsu change check
```

```text
add-role-workflow: spec (2 missing; `kitsu change status add-role-workflow` names them)
reduce-cw-to-evidence-gate: spec (2 missing; `kitsu change status reduce-cw-to-evidence-gate` names them)
add-role-workflow: the files obey their rules
reduce-cw-to-evidence-gate: the files obey their rules
```

Both records stand at the stage `spec`, and that is correct: neither holds a
`reviews.md` with a review and an approval of its specification. Both commands
change no file of the checkout and leave nothing behind; `kitsu change status`
writes temporary files in the git directory and removes them. [The task workflow](workflow.md) says what each stage needs.

## 8. The token count, from a record written by hand

A host writes the record of a session; the plugin gives `kitsu tokens record` a
notice when a role or the session ends. Here you write three small records and
two notices yourself. In `$HOME/kitsu-try/demo`:

```sh
mkdir -p "$HOME/kitsu-try/record"
cd "$HOME/kitsu-try/record"
cat > session.jsonl <<'EOF'
{"type":"user","message":{"role":"user","content":"Please add a greeting."}}
{"type":"assistant","message":{"id":"m1","model":"model-a","usage":{"input_tokens":2000,"cache_creation_input_tokens":3000,"cache_read_input_tokens":14000,"output_tokens":1000}}}
EOF
cat > developer.jsonl <<'EOF'
{"type":"user","message":{"role":"user","content":"kitsu-task: add-greeting code 1\nWrite the greeting."}}
{"type":"assistant","message":{"id":"a1","model":"model-b","usage":{"input_tokens":5000,"cache_creation_input_tokens":5000,"cache_read_input_tokens":28000,"output_tokens":2000}}}
EOF
cat > reviewer.jsonl <<'EOF'
{"type":"user","message":{"role":"user","content":"kitsu-task: add-greeting review 1\nReview the greeting."}}
{"type":"assistant","message":{"id":"b1","model":"model-a","usage":{"input_tokens":2000,"cache_creation_input_tokens":3000,"cache_read_input_tokens":14000,"output_tokens":1000}}}
EOF
cd "$HOME/kitsu-try/demo"
```

```sh
R="$HOME/kitsu-try/record"
echo '{"session_id":"s1","transcript_path":"'"$R"'/session.jsonl","agent_id":"dev1","agent_type":"developer","agent_transcript_path":"'"$R"'/developer.jsonl"}' | kitsu tokens record
echo '{"session_id":"s1","transcript_path":"'"$R"'/session.jsonl","agent_id":"rev1","agent_type":"reviewer","agent_transcript_path":"'"$R"'/reviewer.jsonl"}' | kitsu tokens record
kitsu tokens --task add-greeting
```

The report:

```text
add-greeting: 80,000 tokens in 2 starts of roles
40,000 of 80,000 tokens, 50%, went to the workflow and not to the first writing of the specification, the design, the tests and the code

         writing  40,000
        workflow  40,000
    coordination  20,000
          review  20,000
          lookup       0
             git       0
          rework       0

ROLE       STARTS  INPUT  CACHE WRITE  CACHE READ  OUTPUT  TOTAL   MODEL
reviewer   1       2,000  3,000        14,000      1,000   20,000  model-a
developer  1       5,000  5,000        28,000      2,000   40,000  model-b

STAGE      ROUND   INPUT  CACHE WRITE  CACHE READ  OUTPUT  TOTAL   ROLE
code       1       5,000  5,000        28,000      2,000   40,000  developer
review     1       2,000  3,000        14,000      1,000   20,000  reviewer

session s1: 20,000 tokens of its own, counted as the coordination of add-greeting
workflow tokens are coordination (the session itself), review, lookup, git and rework (a later round of a writing role)
```

The empty line between the two tables holds spaces in the output.

The developer's first round is writing. The review and the session itself are
the workflow: 40,000 of 80,000. Now empty `reviewer.jsonl`
(`: > "$HOME/kitsu-try/record/reviewer.jsonl"`), give the second notice again and
print the report again: the reviewer's total is `unknown`, the report
counts 60,000 known tokens, and it says that the tokens of 1 start are
unknown, so every sum is at least the number shown. An unknown count is never
added as zero without that sentence. [Tokens](tokens.md) explains every number.

## 9. This repository, gated by itself

`kitsu.toml` in the root declares five checks: `build`, `vet`, `test`, `lint` and
`record`. Do not run `kitsu init` in your working clone until you want the gate
there. Try it in a second clone:

```sh
git clone <the root of this checkout> "$HOME/kitsu-try/self"
cd "$HOME/kitsu-try/self"
kitsu init
kitsu run
git commit --allow-empty -m "gated by itself"
```

`kitsu run` can take a few minutes the first time: it builds, vets, tests and
lints the whole module. The commit is made. Then:

```sh
echo "// a change" >> cmd/kitsu/main.go
git commit -am "not verified"
```

This commit is denied, and the message names the five checks.

## 10. With a coding agent

No person has run this part. On 14 November 2026 a script ran some of it in
Claude Code 2.1.311, with `claude -p` and the plugin of this branch, in
scratch repositories:

- **Step 1.** The plugins loaded: the session listed the skills `kitsu` and `task` and, with
  `kitsu-team` installed, the seven roles and the skill `managed`.
- **Step 2.** The text of `kitsu context` reached the model at the start of the
  session.
- **Step 3, in part.** A `git commit --no-verify` did not run. With nobody to
  ask, the host refused the command and gave the reason of the guard.
  `git commit -n` and the search were not tried.
- **Step 4, in part.** In two gated repositories the agent ran `kitsu run` after
  its change and committed through the hook. It did not load the skill `kitsu`
  and did not run `kitsu status` first.
- **Steps 5 and 6.** The roles ran on two small tasks, and the script
  answered every question with one fixed approval:
  [benchmarks](benchmarks.md#what-an-agent-spends).

Nothing was run on Codex. The skill `kitsu` alone was also tried with a model and
without a host: five runs, recorded in
[the evaluation](evaluation.md#a-first-trial-of-the-skill-without-a-host).

1. Start Claude Code in `$HOME/kitsu-try/demo` with the plugin of this checkout,
   from the shell of part 1: `claude --plugin-dir <the root of this checkout>`.
2. The session should start with a short text from `kitsu context` that names
   the gate.
3. Ask it to commit with `--no-verify`, and then with `git commit -n`. It
   should ask you before it runs either command. Ask it to search the
   documents for the text `--no-verify`: it should not ask.
4. Ask for a small fix and a commit, without naming `kitsu`. With the skill
   [`kitsu`](../skills/kitsu/SKILL.md) it should run `kitsu status` first, run
   `kitsu run` after the change, commit through the hook, and quote the last
   line of `kitsu run` in its answer.
5. Install `kitsu-team` and ask `/kitsu-team:managed` to use the **full managed role workflow**, for example to design a new
   check and add a file it guards. Watch whether it writes a record under
   `openspec/changes/<task>/`, whether it stops for your approval of the
   specification, and whether each role starts with the model `kitsu roles`
   prints.
6. Afterwards run `kitsu change status`, `kitsu tokens` and
   `kitsu tokens --task <task>`.

[The evaluation](evaluation.md) lists 26 cases to look at, one for each
scenario that only a model run can show, and the comparison with one agent
working alone. None of them was run. Whether the roles are worth their tokens
is unknown until that comparison is run.

## 11. Clean up

```sh
cd "$HOME/kitsu-try/demo"
kitsu uninstall
```

```text
removed the hooks kitsu wrote, 3 in all
retained check and token records in <your home>/kitsu-try/demo/.git/kitsu
kitsu.toml is left as it is
```

The hooks were in `.git/hooks`, the records in `.git/kitsu`. `kitsu.toml` stays.
`$HOME/kitsu-try` holds everything else this guide created; remove that folder
when you are done.

## What was checked, and what was not

These are historical results for `cw`, before the Kitsu rename; no new Kitsu host run is claimed.

| What | State |
| --- | --- |
| Parts 1 to 9 of this guide | Run on Linux, by hand, with the output shown. |
| The tests of this branch | Run on Linux. [The production plan](../openspec/production-plan.md) lists them. |
| macOS and Windows | The tests passed in CI on both (run 43645026158, commit `3285c99`). On Windows the tests skip the plugin commands written for `sh`. Nothing was tried there by hand. |
| Continuous integration | Run 43645026158, for commit `3285c99` on pull request 219, passed on Linux, macOS and Windows: the tests, the lint, the release archives and the install without Go. A run started by hand on the earlier commit `3eaa40e` also passed with the race detector on all three. |
| Historical release installation | The former `cw v3.0.0-beta.1` was published on 15 November 2026; its release, tag and assets were removed on 17 November. Run 43679683213 tested its then-current README installation on Linux, macOS and Windows without Go, including both Windows PowerShells. The job script also passed on a Linux machine outside CI. Manual macOS/Windows acceptance was not recorded. For current builds, use [CI artifacts](release-install.md#install-a-ci-artifact-before-publication). |
| The plugin in Claude Code | Loaded by a script in Claude Code 2.1.311 (`claude -p --plugin-dir`): the roles and the two skills were listed, the text of `cw context` reached the model, and a `git commit --no-verify` was refused with the reason of the guard. Never used in a session with a person. |
| Codex | Nothing was run. The hooks follow the Codex documentation as read on 14 November 2026. |
| The roles with a model | Run by a script on two small tasks in scratch repositories: [benchmarks](benchmarks.md#what-an-agent-spends). No case of the evaluation was run. |
| The skill `cw` | Its seven examples are replayed by a test against `cw`. Five runs with a model and without a host are in [the evaluation](evaluation.md#a-first-trial-of-the-skill-without-a-host). In two gated scratch repositories the agent ran `cw run` and committed through the hook without loading the skill. Whether a host loads it when it should is not known. |
| The token count of a real session | Both readers were compared with a separate count on real records of this machine: 170 of Claude Code 2.1 and 13 of codex-cli 0.141 and 0.142. In six sessions of Claude Code 2.1.311 with the plugin, two of them with roles, the total of `cw tokens` equalled the total the host reported: [tokens](tokens.md#checked-against-real-records). |

## When something is wrong

Keep the output of the command, `kitsu version` and `kitsu status --json`. A check's
own output is in the log file that `kitsu run` and `kitsu status` name.
