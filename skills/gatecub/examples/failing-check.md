# A check that does not pass

**Situation.** Another repository: `gatecub.toml` declares `unit`, a script that runs the tests, and
`lint`, a program named `service-lint`. Neither check names paths, so each reads every file.
You changed nothing yet and run the checks to learn the state.

```console
$ gatecub run
== unit: sh check.sh
unit tests passed
== unit: source_changed: the source changed during the run: report.txt; when the check writes these files, list them in .gitignore or write them outside the working tree
   log: <repo>/.git/gatecub/logs/<run>.log
== lint: service-lint app
== lint: failed: the program `service-lint` could not be started: exec: "service-lint": executable file not found in $PATH
   log: <repo>/.git/gatecub/logs/<run>.log
not verified: 2 of 2 checks did not pass; fix what each log shows, then run `gatecub run` again
[exit 5]
```

Two checks did not pass, for two different reasons. Neither is a defect in the code under test.

## The check changed a file: source_changed

The tests passed, and the script then wrote `report.txt` into the working tree. `gatecub` records a
pass for the content a check ran on. When that content changes during the run, the pass belongs
to no content, so it does not count.

The file is a product of the check, not a source. Such a file belongs in `.gitignore`: `gatecub`
does not count ignored files as content.

```diff
--- /dev/null
+++ b/.gitignore
@@ -0,0 +1 @@
+report.txt
```

```console
$ gatecub run unit
== unit: sh check.sh
unit tests passed
== unit: passed in 0s
not yet verified; still missing: lint
```

A formatter that rewrites files gives the same outcome. Run the formatter yourself first, then
`gatecub run`, so that the check only confirms the result.

## The program is missing: could not be started

`service-lint` is not installed in this environment. That is a fact about the machine. The
check is right to be there, and no change to the code makes it pass.

Install the program the way the repository documents it, if you are allowed to install things
here. If you are not, stop and tell the user: the check, the line that `gatecub run` printed for
it, and that the commit waits for it.

## The rule for every failure

1. Read what the check printed. The `log:` file holds all of it, also after the terminal is gone.
2. Decide what is wrong: the code, the environment, or the check itself.
3. Wrong code: fix the code and run `gatecub run` again.
4. Wrong environment, such as a missing program, no network, or a timeout on a slow machine:
   fix the environment, or tell the user.
5. Wrong check, such as a test that asserts the old behaviour: say so and ask. The checks and
   `gatecub.toml` are the user's. Do not delete a test, mark it skipped, switch a lint rule off, or
   raise a timeout to get past the gate.
6. A check that failed before your change: tell the user at once. Do not hide the repair inside
   your own change.
7. Nobody is there to ask: finish what you can, leave it uncommitted, and report the check,
   what it printed, and the command that completes the commit once the cause is gone.

<!-- A test replays this example from the fixture "service": internal/repocheck/skill_test.go -->
