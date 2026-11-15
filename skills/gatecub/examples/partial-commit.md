# A commit of a part of the changes

**Situation.** Two files are changed in the working tree: a comment in `src/total.sh` and a new
test in `tests/run.sh`. You want the first commit to hold only the comment.

The hook judges what the commit will hold. That is the index, not the files on disk.

```diff
--- a/tests/run.sh
+++ b/tests/run.sh
@@ -4,3 +4,4 @@
 }
 check "two prices" 5 "$(total 2 3)"
+check "no price twice" 4 "$(total 2 2)"
 echo "tests passed"
```

```diff
--- a/src/total.sh
+++ b/src/total.sh
@@ -1,4 +1,4 @@
-# total prints the sum of two prices.
+# total prints the sum of two whole prices.
 total() {
   echo $(( $1 + $2 ))
 }
```

```console
$ gatecub run
== test: sh tests/run.sh
tests passed
== test: passed in 0s
== lint: sh -n src/total.sh
== lint: passed in 0s
every declared check has a passing run on this content
$ git add src/total.sh
$ git commit -q -m "shorten the comment"
gatecub: this commit is denied: its content has no recorded passing run of every declared check.
  test: not run on this content
  The working tree holds other content than this commit. Paths that differ:
    tests/run.sh
  The working tree as a whole is verified; this commit is not, because it records other content.
gatecub: `gatecub run` checks the working tree as it is on disk. Make the working tree hold exactly what you commit, by staging the rest (git add -A) or by setting it aside (git stash push --keep-index --include-untracked), and run:
  gatecub run
[exit 1]
$ git add -A
$ git commit -q -m "shorten the comment, test equal prices"
```

`gatecub run` passed on the two changes together. No run ever saw the staged half alone, so `test`,
which reads both files, has no evidence for it. `lint` reads only `src/`, and the staged content
of `src/` did pass, so `lint` is not named.

## What to do

The simple way is the one above: commit the two changes together. They passed together.

When they must be two commits, make the working tree hold exactly the first one, by setting the
other change aside in the way you normally do, for example on a branch of its own or as a
patch file outside the repository. Then run `gatecub run`, commit, bring the other change back, run
`gatecub run` again and commit that. Each commit then has evidence for exactly what it holds.

A change to a file that no check reads does not need this: the hook allows the commit, because
the content the checks read is the same with or without that file.

<!-- A test replays this example from the fixture "shop": internal/repocheck/skill_test.go -->
