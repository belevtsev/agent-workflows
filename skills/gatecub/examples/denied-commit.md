# git denies a commit

**Situation.** You changed a file and committed at once. The same answer comes for a push, with
"push" in place of "commit".

```diff
--- a/src/total.sh
+++ b/src/total.sh
@@ -1,4 +1,4 @@
-# total prints the sum of two prices.
+# total prints the sum of two prices, as a whole number.
 total() {
   echo $(( $1 + $2 ))
 }
```

```console
$ git add -A
$ git commit -q -m "say what total prints"
gatecub: this commit is denied: its content has no recorded passing run of every declared check.
  test: not run on this content
  lint: not run on this content
gatecub: to produce the missing evidence, run:
  gatecub run
[exit 1]
```

Nothing is broken. The hook looked for a passing run of each check on the content you staged
and found none, because that content is new. Even a changed comment is new content.

## What to do

Do what the message says, then commit again with the same command.

```console
$ gatecub run
== test: sh tests/run.sh
tests passed
== test: passed in 0s
== lint: sh -n src/total.sh
== lint: passed in 0s
every declared check has a passing run on this content
$ git commit -q -m "say what total prints"
```

## What not to do

Do not pass `--no-verify`, do not change or remove a hook, and do not write under `.git/gatecub/`.
Each of these makes a commit that looks checked and is not, and the user can no longer tell the
two apart. If `gatecub run` cannot pass, read [a check that does not pass](failing-check.md). If the
commit should hold only a part of your changes, read [a part of the changes](partial-commit.md).

<!-- A test replays this example from the fixture "shop": internal/repocheck/skill_test.go -->
