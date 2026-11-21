---
name: kitsu
description: 'Set up kitsu.toml checks and the Git gate, or diagnose failed checks, "not verified" and "kitsu: this commit is denied". Use the task skill for coding tasks.'
---

# Working in a repository gated by kitsu

`kitsu` is one program for any git repository in any language. The repository declares its checks
in `kitsu.toml`: a test run, a linter, a build, any program that exits 0 when the content is good.
`kitsu run` executes them and records each result for the exact content it ran on. The git hooks
execute nothing. They look the record up, and they deny a commit or a push whose content has
no passing run of every check.

Prefer grouped commands such as `kitsu gate status`, `kitsu check run`, and `kitsu task check`.
Console transcripts here and in the examples retain valid legacy aliases with the same gate
and evidence behavior. `kitsu help --all` lists both surfaces.

## The five rules

1. **Look first.** Run `kitsu status` before you change anything, so you know whether a later
   failure is yours.
2. **Run before you commit.** `kitsu run` executes only the checks whose files changed, so run it
   after each step of the work. If the plugin requires approval, wait for the developer to run
   `kitsu check trust` in their terminal.
3. **Never go around the gate.** No `--no-verify`, no change to a hook, nothing written under
   `.git/kitsu/`. A commit that skipped the gate looks the same as one that passed it, and the
   user can no longer tell which of your commits were checked.
4. **Make a check pass by fixing what it found.** Do not delete or skip a test, switch a lint
   rule off, raise a timeout or edit `kitsu.toml` to get a pass. The checks are the user's. If a
   check is wrong, or cannot pass on this machine, say so and ask. When nobody is there to
   ask, leave the work uncommitted and report what is missing.
5. **Report what the program printed.** Say "verified" only when the last `kitsu run` or
   `kitsu status` said so on the content as it is now. Quote its last line.

## Before you start: is kitsu here?

Run `kitsu status` and act on the first row that matches what it prints.

| You see | It means | You do |
| --- | --- | --- |
| `command not found` | `kitsu` is not installed | Tell the user; the commits here will be denied until it is. Do not install it unasked. |
| `the tree has no declaration` | No `kitsu.toml`: the repository is not gated | Work as usual. Set a gate up only when the user asks: [a repository without checks](examples/new-repository.md). |
| `verified` | Every check passed on this content | Start the work. |
| `not verified`, and a check `failed` | Something was broken before you came | Tell the user before you change anything. |
| `not verified`, and checks `not run on this content` | Nobody ran them on this content yet | `kitsu run` now, so that you know the starting state. |
| a warning that the hooks `are not installed in this clone` | A new clone or worktree: git checks nothing here | `kitsu init`, then `kitsu run`: [a new clone](examples/fresh-clone.md). |
| `mode: observe` | The hooks record and allow everything | Work in the same way; the team is trying the gate out. |

## The loop for any task

1. `kitsu status`. Know the state you start from.
2. Make one step of the change. Where behaviour changes, write the test first and see it fail
   with `kitsu run`: a test that never failed shows nothing.
3. `kitsu run`. Read what did not pass, fix the cause, run again. Repeat until the last line is
   `every declared check has a passing run on this content`.
4. When a commit was requested, inspect and stage its intended changes, then commit. The hook checks the staged tree.
5. Tell the user what changed, and quote the last line of `kitsu run`.

A change that is small and plain needs nothing more than this loop:
[a small change](examples/small-change.md) shows it from the first look to the commit.

## Reading the output

In the examples, `[exit 5]` under an output is the exit status of the command, shown when it is
not 0: 0 done, 1 could not, 2 wrong command line, 5 not verified.

| Output | Meaning | What to do |
| --- | --- | --- |
| `satisfied, not run again` | A passing run on this content is recorded already | Nothing. Do not add `--force`. |
| `failed with exit status N`, then `log:` | The check ran and found something | Read the output, or the log file it names, and fix the cause. |
| `source_changed` | The check passed, and it changed files in the working tree, so the pass counts for no content | Make the check stop writing into the tree, or have git ignore what it writes. |
| `could not be started` | The program of the check is not installed here | Install it as the repository says, or tell the user. |
| `timed out` | The check ran longer than `kitsu.toml` allows | Find out why it is slow. The timeout is not yours to raise. |
| `not run on this content` | The content changed since the last run, or no run was made | `kitsu run`. |
| `kitsu: this commit is denied` | The content you staged has no passing run of every check | `kitsu run`, then commit again: [a denied commit](examples/denied-commit.md). |

[A check that does not pass](examples/failing-check.md) shows the cases that are not a plain
defect in the code, and the rule for each.

## Mistakes that agents make here

- **Editing after the last run.** One more edit to a file a check reads, even to a comment,
  makes new content. Run `kitsu run` again before the commit.
- **Staging a part.** The hook judges what is staged. If you stage one of two changed files
  that a check reads, no run ever saw that state. Stage all of it with `git add -A`, or see
  [a part of the changes](examples/partial-commit.md).
- **Leaving scratch files in the repository.** A file git does not ignore is part of the
  content, tracked or not. Keep notes and trial scripts outside the repository.
- **Running `kitsu run --force` to be sure.** A recorded pass on the same content is the same
  answer. `--force` only spends the time again.
- **Repairing the check instead of the code.** See rule 4.
- **Saying "the checks pass" from memory.** An earlier pass says nothing about the content as
  it is now. Run `kitsu status` and quote it.
- **Guessing an option.** `kitsu help` lists every command, and `kitsu <command> --help` its options.

## Changing what is checked

`kitsu.toml` belongs to the user. Add or change a check only when they ask, with
`kitsu add <id> -- <program> [<argument>...]`, and say what you added.
[A repository without checks](examples/new-repository.md) shows how to find the right commands
in a repository of any language, and what to tell the user about clones and about continuous
integration.

## A larger task

For new behaviour that deserves a written specification, or a change across several parts of
the code, the opt-in plugin `kitsu-team` has the skill `managed`: seven roles take the task from a
specification through tests to code, a review and a gated commit. It uses several times the tokens
of the loop, so propose it and let the user choose.

## The examples

Each example gives a situation, the commands, what they printed and the reason. The projects in
them are small on purpose. Take the method from them, not the names.

- [A small change](examples/small-change.md): read it when you start a change here.
- [A denied commit](examples/denied-commit.md): read it when git prints
  `kitsu: this commit is denied`.
- [A part of the changes](examples/partial-commit.md): read it when a commit should hold only
  some of the files you changed.
- [A check that does not pass](examples/failing-check.md): read it when `kitsu run` ends without
  a pass and the cause is not a plain defect in your change.
- [A repository without checks](examples/new-repository.md): read it when the user asks for
  checks or a gate and `kitsu status` says `the tree has no declaration`.
- [A new clone](examples/fresh-clone.md): read it when `kitsu status` warns that the hooks are
  missing, or `kitsu init` wrote no hook.
