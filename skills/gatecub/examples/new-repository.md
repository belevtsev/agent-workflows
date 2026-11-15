# A repository that has no checks declared yet

**Situation.** The user asks: "Make sure nothing gets committed here unless the tests pass."
The repository holds code and a test script and no `gatecub.toml`.

```console
$ gatecub status
gatecub: the tree has no declaration: there is no gatecub.toml at its root; `gatecub init` creates one
[exit 1]
```

This is not an error to repair on your own. It says the repository is not gated. Set the gate up
only because the user asked for it.

## 1. Find the commands this repository already uses

Do not invent a check. Look for the command that the people here already run, in this order:

1. the continuous integration files, such as `.github/workflows/`;
2. the task file of the build tool: `Makefile`, the `scripts` of `package.json`, `justfile`,
   `tox.ini`, `pyproject.toml`;
3. the README and the contributing guide.

A check is any program that exits 0 when the content is good. Typical ones, for orientation
only: `go test ./...`, `cargo test`, `npm test`, `pytest`, `mvn -q verify`, `dotnet test`,
`ruff check .`, `eslint .`, `golangci-lint run`, `tsc --noEmit`. In this example the repository
has one script, `tests/greet_test.sh`.

## 2. Declare it

```console
$ gatecub init
created gatecub.toml with no checks
installed the hooks pre-commit, pre-merge-commit, pre-push
the gate enforces: a commit or a push needs a passing run of every declared check
next: gatecub add <id> -- <program> [<argument>...], then gatecub run
$ gatecub add test --paths 'app/**' --paths 'tests/**' -- sh tests/greet_test.sh
added the check `test` to gatecub.toml
```

`--paths` names the files a check reads. A change to any other file does not make the check
run again. Leave `--paths` out when you are not sure: the check then reads everything, which is
slower and never wrong. Everything after `--` is the program and its arguments; `gatecub` starts it
without a shell, so a pipe or `&&` needs `sh -c '...'`.

## 3. Run, and commit the declaration

```console
$ gatecub run
== test: sh tests/greet_test.sh
greet tests passed
== test: passed in 0s
every declared check has a passing run on this content
$ git add -A
$ git commit -q -m "gate commits on the tests"
$ gatecub status
verified: every declared check has a passing run on this content
  test  satisfied
index: holds the same content as the working tree
```

If the first run ends `source_changed`, the check wrote files into the working tree, such as a
cache or a report. Have git ignore them: [a check that does not pass](failing-check.md).

## What to tell the user

- `gatecub.toml` is committed, so everyone gets the same checks. The hooks are not: git keeps hooks
  in each clone, so each person runs `gatecub init` once after cloning.
- A team that wants to watch first can start with `gatecub init --observe`: the hooks then record
  what they would deny and allow everything. `gatecub stats` shows the count.
- A local hook can be skipped by anyone who wants to. To hold the rule for a whole team, run
  `gatecub run --base <revision>` in continuous integration and make that job required. The
  operations guide of gatecub has the lines for it.
- The user can see a denial for themselves by changing a file and committing without `gatecub run`.
  You do not need to break code to demonstrate it.
- If another tool already owns the hooks of the repository, `gatecub init` leaves them alone and
  prints the one line to add to each.

<!-- A test replays this example from the fixture "plain": internal/repocheck/skill_test.go -->
