# Quick start

Five commands take a repository from nothing to a gated commit. A test,
`TestQuickStart` in `internal/repocheck`, executes exactly the commands of the
two blocks below in a new directory, so the commands of this page cannot
drift from the program.

You need `kitsu` and git on your `PATH`; [the README](../README.md#install) says
how to get `kitsu`. git must know your name and e-mail, as for any commit: when
`git config user.name` or `git config user.email` prints nothing, set it first.

## A new repository

```sh
git init demo
cd demo
```

In a repository you already have, skip this and start with the five commands.

## The five commands

```sh
kitsu init
kitsu add version -- git --version
kitsu run
git add -A
git commit -m "first gated commit"
```

1. `kitsu init` writes `kitsu.toml` with no check yet and installs the hooks
   `pre-commit`, `pre-merge-commit` and `pre-push`.
2. `kitsu add` appends one check to `kitsu.toml`: an id, and after `--` the program
   to run with its arguments. `git --version` is only an example that works
   everywhere. Use the command that tests your code, for instance
   `kitsu add test -- make test`. Add one check for each command you want to
   pass: the tests, the linter, the build.
3. `kitsu run` executes every check that has no passing run on the content of the
   working tree, shows its output, and records the result for exactly that
   content.
4. `git add -A` stages everything, so the commit holds what was checked.
5. `git commit` runs the hook. The hook runs no check. It looks for a passing
   run of every declared check on the content the commit would record, finds
   it, and allows the commit.

## What a denial looks like

Change a file and commit without `kitsu run`:

```text
$ echo change >> notes.txt && git add -A && git commit -m second
kitsu: this commit is denied: its content has no recorded passing run of every declared check.
  version: not run on this content
kitsu: to produce the missing evidence, run:
  kitsu run
```

Run `kitsu run` and commit again. `kitsu status` says at any time whether a commit
of the working tree would be allowed, and what is missing.

## Next

- [Operations](operations.md): every command and every exit status.
- [Hooks](hooks.md): what the three hooks decide, and how to share a hook
  with another tool.
- [Evidence](evidence.md): what a recorded run proves, and what it does not.
- [The task workflow](workflow.md): the roles, for work with a coding agent.
