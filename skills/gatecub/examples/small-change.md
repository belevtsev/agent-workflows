# A small change, from the first look to the commit

**Situation.** The repository holds `gatecub.toml` with two checks: `test`, which reads `src/` and
`tests/`, and `lint`, which reads `src/`. The user asks: "`total` must add any number of prices,
not only two." The change is small and its cause is plain, so it needs no roles: the loop below
is the whole method.

## 1. Look before you change anything

```console
$ gatecub status
verified: every declared check has a passing run on this content
  test  satisfied
  lint  satisfied
index: holds the same content as the working tree
```

The repository is verified now, so a check that fails later fails because of your change.

## 2. Write the test first, and see it fail

```diff
--- a/tests/run.sh
+++ b/tests/run.sh
@@ -4,3 +4,4 @@
 }
 check "two prices" 5 "$(total 2 3)"
+check "three prices" 6 "$(total 1 2 3)"
 echo "tests passed"
```

```console
$ gatecub run
== test: sh tests/run.sh
three prices: want 6, got 3
== test: failed with exit status 1
   log: <repo>/.git/gatecub/logs/<run>.log
== lint: satisfied, not run again: run <run> passed on this content
not verified: 1 of 2 checks did not pass; fix what each log shows, then run `gatecub run` again
[exit 5]
```

The failure is the one you wanted: it shows that the test can tell the old behaviour from the
new one. `lint` was not executed, because nothing it reads has changed. That is normal.

## 3. Change the code, and run again

```diff
--- a/src/total.sh
+++ b/src/total.sh
@@ -1,4 +1,6 @@
-# total prints the sum of two prices.
+# total prints the sum of its prices.
 total() {
-  echo $(( $1 + $2 ))
+  sum=0
+  for price in "$@"; do sum=$(( sum + price )); done
+  echo "$sum"
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
```

## 4. Commit

```console
$ git add -A
$ git commit -q -m "total adds any number of prices"
$ gatecub status
verified: every declared check has a passing run on this content
  test  satisfied
  lint  satisfied
index: holds the same content as the working tree
```

The hook allowed the commit without running anything: it found the two passing runs on exactly
this content. Tell the user what you changed and quote the last line of `gatecub run`.

## What to take from this

- `gatecub status` first, `gatecub run` after each step, commit last.
- A check that reads none of the changed files is not run again.
- After the last `gatecub run`, change nothing a check reads. One more edit makes new content, and
  the commit is denied until `gatecub run` passes on it.

<!-- A test replays this example from the fixture "shop": internal/repocheck/skill_test.go -->
