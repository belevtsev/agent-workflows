# Quick start

Five commands take a repository from nothing to a gated commit. A test,
`TestQuickStart` in `internal/repocheck`, executes exactly the commands of the
two blocks below in a new directory, so the commands of this page cannot
drift from the program.

You need `gatecub` and git on your `PATH`; [the README](../README.md#install) says
how to get `gatecub`. git must know your name and e-mail, as for any commit: when
`git config user.name` or `git config user.email` prints nothing, set it first.

## A new repository

```sh
git init demo
cd demo
```

In a repository you already have, skip this and start with the five commands.

## The five commands

```sh
gatecub init
gatecub add version -- git --version
gatecub run
git add -A
git commit -m "first gated commit"
```

1. `gatecub init` writes `gatecub.toml` with no check yet and installs the hooks
   `pre-commit`, `pre-merge-commit` and `pre-push`.
2. `gatecub add` appends one check to `gatecub.toml`: an id, and after `--` the program
   to run with its arguments. `git --version` is only an example that works
   everywhere. Use the command that tests your code, for instance
   `gatecub add test -- make test`. Add one check for each command you want to
   pass: the tests, the linter, the build.
3. `gatecub run` executes every check that has no passing run on the content of the
   working tree, shows its output, and records the result for exactly that
   content.
4. `git add -A` stages everything, so the commit holds what was checked.
5. `git commit` runs the hook. The hook runs no check. It looks for a passing
   run of every declared check on the content the commit would record, finds
   it, and allows the commit.

## What a denial looks like

Change a file and commit without `gatecub run`:

```text
$ echo change >> notes.txt && git add -A && git commit -m second
gatecub: this commit is denied: its content has no recorded passing run of every declared check.
  version: not run on this content
gatecub: to produce the missing evidence, run:
  gatecub run
```

Run `gatecub run` and commit again. `gatecub status` says at any time whether a commit
of the working tree would be allowed, and what is missing.

## Next

- [Operations](operations.md): every command and every exit status.
- [Hooks](hooks.md): what the three hooks decide, and how to share a hook
  with another tool.
- [Evidence](evidence.md): what a recorded run proves, and what it does not.
- [The task workflow](workflow.md): the roles, for work with a coding agent.
