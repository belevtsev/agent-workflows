# A new clone, where the hooks are not installed

**Situation.** You start work in a clone that somebody made a minute ago, or in a new worktree
of a tool that keeps its own hooks directory. The repository holds `cw.toml`.

```console
$ cw status
cw: warning: the hooks of cw are not installed in this clone, so git gates nothing here; run `cw init`
not verified: a commit of this content would be denied
  test  not run on this content
  lint  not run on this content
index: holds the same content as the working tree
run `cw run` to produce the missing evidence
[exit 5]
```

Two separate things are true here.

- **The hooks are missing.** git never copies hooks with a clone, so in this clone a commit
  would go through unchecked. The repository still expects the checks.
- **Nothing is recorded.** The records of passing runs live in the clone as well, under
  `.git/cw/`, so a new clone starts with none, even for content that passed elsewhere.

## What to do

Install the hooks, then produce the evidence. `cw init` keeps the `cw.toml` that is there.

```console
$ cw init
cw.toml is kept as it is
installed the hooks pre-commit, pre-merge-commit, pre-push
the gate enforces: a commit or a push needs a passing run of every declared check
next: cw add <id> -- <program> [<argument>...], then cw run
$ cw run
== test: sh tests/run.sh
tests passed
== test: passed in 0s
== lint: sh -n src/total.sh
== lint: passed in 0s
every declared check has a passing run on this content
$ cw status
verified: every declared check has a passing run on this content
  test  satisfied
  lint  satisfied
index: holds the same content as the working tree
```

Installing the hooks changes this clone only, and `cw uninstall` takes them out again, so you
may do it without asking. Say that you did.

## When cw init writes no hook

If the clone is set to take its hooks from another directory (`core.hooksPath`), or another
tool already wrote a hook file, `cw init` does not overwrite anything. It prints the one line to
add to each hook, and it exits with status 4. Those files belong to someone else: show the user
the line and let them decide. Until the line is there, run `cw run` before each commit all the
same, so that the content you commit has its evidence.

<!-- A test replays this example from the fixture "clone": internal/repocheck/skill_test.go -->
