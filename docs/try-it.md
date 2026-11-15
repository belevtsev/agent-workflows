# Try it

This guide takes you through everything on this branch that can be tried by
hand. Parts 1 to 9 need no coding agent and take about twenty minutes. Every
command and every output in parts 1 to 9 was run on Linux, on 14 November
2026, with the program built from this branch. Part 10 was run in part, by a
script and not by a person; it says which steps.

Nothing here replaces a `cw` you already have installed, and nothing here
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
mkdir -p "$HOME/cw-try"
go build -o "$HOME/cw-try/cw" ./cmd/cw
export PATH="$HOME/cw-try:$PATH"
cw version
```

Everything this guide creates is under `$HOME/cw-try`. If that folder exists
already, pick another, such as one from `mktemp -d`, and use it in place of
`$HOME/cw-try` below.

The first line must start with `cw v3.0.0`. If it does not, another `cw` is
still first on your `PATH`.

Stay in this shell for every part below. The git hooks call `cw` from `PATH`,
so a shell without the `export` line would call the program you had before.

## 2. The five commands, in a new repository

```sh
git init "$HOME/cw-try/demo"
cd "$HOME/cw-try/demo"
cw init
cw add no-bad -- test ! -e BAD
cw run
git add -A
git commit -m "first gated commit"
```

`cw init` prints:

```text
created cw.toml with no checks
installed the hooks pre-commit, pre-merge-commit, pre-push
the gate enforces: a commit or a push needs a passing run of every declared check
next: cw add <id> -- <program> [<argument>...], then cw run
```

`cw add` prints ``added the check `no-bad` to cw.toml``. `cw run` prints:

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
cw: this commit is denied: its content has no recorded passing run of every declared check.
  no-bad: not run on this content
cw: to produce the missing evidence, run:
  cw run
```

`cw status` says the same without a commit, and exits 5:

```text
not verified: a commit of this content would be denied
  no-bad  not run on this content
index: holds the same content as the working tree
run `cw run` to produce the missing evidence
```

Now produce the evidence:

```sh
cw run
git commit -m "verified now"
```

This commit is made.

## 4. A check that fails, and a passing run that is used again

```sh
touch BAD
cw run
```

`cw run` exits 5 and names the log of the failed run:

```text
== no-bad: test ! -e BAD
== no-bad: failed with exit status 1
   log: <your home>/cw-try/demo/.git/cw/logs/<time>-<id>.log
not verified: 1 of 1 checks did not pass; fix what each log shows, then run `cw run` again
```

`git add -A` and `git commit -m "fails"` are denied:

```text
cw: this commit is denied: its content has no recorded passing run of every declared check.
  no-bad: failed with exit status 1; log: <your home>/cw-try/demo/.git/cw/logs/<time>-<id>.log
cw: a check failed or timed out; first fix what its log shows.
cw: to produce the missing evidence, run:
  cw run
```

Take the file away again:

```sh
git rm -f --cached BAD
mv BAD "$HOME/cw-try/BAD"
cw status
```

`cw status` prints `verified` without any new run: the content is again the
content of part 3, and that content has a passing run on record.

## 5. What the gate recorded

```sh
cw stats
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
cw roles
cw roles text developer
```

`cw roles` prints the seven roles with their models on Claude Code and on
Codex, the size of each text and what each role may write. `cw roles text`
prints the text a model is given for one role. Read one or two of them: they
are the product, and no program can check their quality.

`cw roles check` compares the working tree with an earlier snapshot of it. Take
the snapshot, then act as a `reviewer` that left a file behind:

```sh
cw status --json
echo change > review-notes.txt
cw roles check reviewer --from <the value of "tree" in the JSON>
```

Take the first `"tree"` of the JSON, the one beside `"verified"`. A check
holds a `"tree"` of its own: the content its last run was on.

It exits 5:

```text
`reviewer` changed 1 of 1 paths outside its limit:
  review-notes.txt (added): `reviewer` may change nothing
`reviewer` writes nothing: ask the developer to remove these paths, or remove them after their yes; do not accept the work
```

Move the file out (`mv review-notes.txt "$HOME/cw-try/"`) and run the same
command again: it exits 0 and prints that the role stayed within its limit.

The command sees which paths changed. It cannot see who changed them.

## 7. The record of a task

This repository keeps two task records under `openspec/changes/`. In the root
of this checkout, in the same shell:

```sh
cw change status
cw change check
```

```text
add-role-workflow: spec (2 missing; `cw change status add-role-workflow` names them)
reduce-cw-to-evidence-gate: spec (2 missing; `cw change status reduce-cw-to-evidence-gate` names them)
add-role-workflow: the files obey their rules
reduce-cw-to-evidence-gate: the files obey their rules
```

Both records stand at the stage `spec`, and that is correct: neither holds a
`reviews.md` with a review and an approval of its specification. Both commands
change no file of the checkout and leave nothing behind; `cw change status`
writes temporary files in the git directory and removes them. [The task workflow](workflow.md) says what each stage needs.

## 8. The token count, from a record written by hand

A host writes the record of a session; the plugin gives `cw tokens record` a
notice when a role or the session ends. Here you write three small records and
two notices yourself. In `$HOME/cw-try/demo`:

```sh
mkdir -p "$HOME/cw-try/record"
cd "$HOME/cw-try/record"
cat > session.jsonl <<'EOF'
{"type":"user","message":{"role":"user","content":"Please add a greeting."}}
{"type":"assistant","message":{"id":"m1","model":"model-a","usage":{"input_tokens":2000,"cache_creation_input_tokens":3000,"cache_read_input_tokens":14000,"output_tokens":1000}}}
EOF
cat > developer.jsonl <<'EOF'
{"type":"user","message":{"role":"user","content":"cw-task: add-greeting code 1\nWrite the greeting."}}
{"type":"assistant","message":{"id":"a1","model":"model-b","usage":{"input_tokens":5000,"cache_creation_input_tokens":5000,"cache_read_input_tokens":28000,"output_tokens":2000}}}
EOF
cat > reviewer.jsonl <<'EOF'
{"type":"user","message":{"role":"user","content":"cw-task: add-greeting review 1\nReview the greeting."}}
{"type":"assistant","message":{"id":"b1","model":"model-a","usage":{"input_tokens":2000,"cache_creation_input_tokens":3000,"cache_read_input_tokens":14000,"output_tokens":1000}}}
EOF
cd "$HOME/cw-try/demo"
```

```sh
R="$HOME/cw-try/record"
echo '{"session_id":"s1","transcript_path":"'"$R"'/session.jsonl","agent_id":"dev1","agent_type":"developer","agent_transcript_path":"'"$R"'/developer.jsonl"}' | cw tokens record
echo '{"session_id":"s1","transcript_path":"'"$R"'/session.jsonl","agent_id":"rev1","agent_type":"reviewer","agent_transcript_path":"'"$R"'/reviewer.jsonl"}' | cw tokens record
cw tokens --task add-greeting
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
(`: > "$HOME/cw-try/record/reviewer.jsonl"`), give the second notice again and
print the report again: the reviewer's total is `unknown`, the report
counts 60,000 known tokens, and it says that the tokens of 1 start are
unknown, so every sum is at least the number shown. An unknown count is never
added as zero without that sentence. [Tokens](tokens.md) explains every number.

## 9. This repository, gated by itself

`cw.toml` in the root declares five checks: `build`, `vet`, `test`, `lint` and
`record`. Do not run `cw init` in your working clone until you want the gate
there. Try it in a second clone:

```sh
git clone <the root of this checkout> "$HOME/cw-try/self"
cd "$HOME/cw-try/self"
cw init
cw run
git commit --allow-empty -m "gated by itself"
```

`cw run` can take a few minutes the first time: it builds, vets, tests and
lints the whole module. The commit is made. Then:

```sh
echo "// a change" >> cmd/cw/main.go
git commit -am "not verified"
```

This commit is denied, and the message names the five checks.

## 10. With a coding agent

No person has run this part. On 14 November 2026 a script ran some of it in
Claude Code 2.1.311, with `claude -p` and the plugin of this branch, in
scratch repositories:

- **Step 1.** The plugin loaded: the session listed the seven roles and the
  skills `cw` and `task`.
- **Step 2.** The text of `cw context` reached the model at the start of the
  session.
- **Step 3, in part.** A `git commit --no-verify` did not run. With nobody to
  ask, the host refused the command and gave the reason of the guard.
  `git commit -n` and the search were not tried.
- **Step 4, in part.** In two gated repositories the agent ran `cw run` after
  its change and committed through the hook. It did not load the skill `cw`
  and did not run `cw status` first.
- **Steps 5 and 6.** The roles ran on two small tasks, and the script
  answered every question with one fixed approval:
  [benchmarks](benchmarks.md#what-an-agent-spends).

Nothing was run on Codex. The skill `cw` alone was also tried with a model and
without a host: five runs, recorded in
[the evaluation](evaluation.md#a-first-trial-of-the-skill-without-a-host).

1. Start Claude Code in `$HOME/cw-try/demo` with the plugin of this checkout,
   from the shell of part 1: `claude --plugin-dir <the root of this checkout>`.
2. The session should start with a short text from `cw context` that names
   the gate.
3. Ask it to commit with `--no-verify`, and then with `git commit -n`. It
   should ask you before it runs either command. Ask it to search the
   documents for the text `--no-verify`: it should not ask.
4. Ask for a small fix and a commit, without naming `cw`. With the skill
   [`cw`](../skills/cw/SKILL.md) it should run `cw status` first, run
   `cw run` after the change, commit through the hook, and quote the last
   line of `cw run` in its answer.
5. Ask for a small task with `/agent-workflows:task`, for example a new
   check and a file it guards. Watch whether it writes a record under
   `openspec/changes/<task>/`, whether it stops for your approval of the
   specification, and whether each role starts with the model `cw roles`
   prints.
6. Afterwards run `cw change status`, `cw tokens` and
   `cw tokens --task <task>`.

[The evaluation](evaluation.md) lists 26 cases to look at, one for each
scenario that only a model run can show, and the comparison with one agent
working alone. None of them was run. Whether the roles are worth their tokens
is unknown until that comparison is run.

## 11. Clean up

```sh
cd "$HOME/cw-try/demo"
cw uninstall
```

```text
removed the hooks cw wrote, 3 in all, and the records of cw in <your home>/cw-try/demo/.git/cw
cw.toml is left as it is
```

The hooks were in `.git/hooks`, the records in `.git/cw`. `cw.toml` stays.
`$HOME/cw-try` holds everything else this guide created; remove that folder
when you are done.

## What was checked, and what was not

| What | State |
| --- | --- |
| Parts 1 to 9 of this guide | Run on Linux, by hand, with the output shown. |
| The tests of this branch | Run on Linux. [The production plan](../openspec/production-plan.md) lists them. |
| macOS and Windows | The tests passed in CI on both (run 43645026158, commit `3285c99`). On Windows the tests skip the plugin commands written for `sh`. Nothing was tried there by hand. |
| Continuous integration | Run 43645026158, for commit `3285c99` on pull request 219, passed on Linux, macOS and Windows: the tests, the lint, the release archives and the install without Go. A run started by hand on the earlier commit `3eaa40e` also passed with the race detector on all three. |
| A release, and the install without Go | The release `v3.0.0-beta.1` is published. The release workflow (run 43679683213) followed the first install instruction of the README against it on Linux, macOS and Windows, on a PATH without Go, on Windows in both PowerShells. The script of that job also passed once on a Linux machine outside CI. Nobody followed the instruction by hand on macOS or Windows. |
| The plugin in Claude Code | Loaded by a script in Claude Code 2.1.311 (`claude -p --plugin-dir`): the roles and the two skills were listed, the text of `cw context` reached the model, and a `git commit --no-verify` was refused with the reason of the guard. Never used in a session with a person. |
| Codex | Nothing was run. The hooks follow the Codex documentation as read on 14 November 2026. |
| The roles with a model | Run by a script on two small tasks in scratch repositories: [benchmarks](benchmarks.md#what-an-agent-spends). No case of the evaluation was run. |
| The skill `cw` | Its seven examples are replayed by a test against `cw`. Five runs with a model and without a host are in [the evaluation](evaluation.md#a-first-trial-of-the-skill-without-a-host). In two gated scratch repositories the agent ran `cw run` and committed through the hook without loading the skill. Whether a host loads it when it should is not known. |
| The token count of a real session | Both readers were compared with a separate count on real records of this machine: 170 of Claude Code 2.1 and 13 of codex-cli 0.141 and 0.142. In six sessions of Claude Code 2.1.311 with the plugin, two of them with roles, the total of `cw tokens` equalled the total the host reported: [tokens](tokens.md#checked-against-real-records). |

## When something is wrong

Keep the output of the command, `cw version` and `cw status --json`. A check's
own output is in the log file that `cw run` and `cw status` name.
