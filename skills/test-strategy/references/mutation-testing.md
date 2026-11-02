# Mutation testing in Go

Read this when you need to know whether tests would actually fail if the code broke. Coverage shows what ran. Mutation testing changes the code and checks that a test notices. Use it on important or recently changed packages, not across a whole repository, and report a missing tool rather than installing it unasked.

This is source-aware verifier/reviewer work. Under the [test-author protocol](../../task-orchestration/references/test-author.md), authors do not inspect mutants, production source, coverage, or raw mutation/build/runtime output. The verifier translates a survivor into a requirement/case ID and contract input with expected/actual outcomes, or asks the coordinator to resolve an unspecified contract. New or changed tests, helpers, fixtures, skips, tolerances, and seeds return to an unexposed author; do not send mutant text or implementation-derived expected values.

## Run Gremlins

[Gremlins](https://github.com/go-gremlins/gremlins) v0.6.0 is the practical choice. It's an external binary, works in temporary directories, and doesn't change your source.

```sh
go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
GOFLAGS=-count=1 gremlins unleash ./internal/outcomes --workers 4 --timeout-coefficient 5 -o gremlins.json
jq -r '.files[] | .file_name as $f | .mutations[] | select(.status=="LIVED") | "\($f):\(.line) \(.type)"' gremlins.json
```

**Always set `GOFLAGS=-count=1`.** Gremlins sets each mutant's timeout from a baseline test run, and a cached `go test` result makes that baseline look instant. Mutants then hit a tiny timeout, and efficacy ignores timeouts. The same package gave these results:

| Run | Killed | Lived | Timed out | Reported efficacy |
| --- | --- | --- | --- | --- |
| With `GOFLAGS=-count=1` | 164 | 21 | 1 | 88.65% |
| With a cached baseline | 3 | 0 | 183 | 100.00% |

**Treat a run where timeouts outnumber kills as invalid,** however good its score looks.

## Known limits

- Non-compiling mutants are reported as killed ([#321](https://github.com/go-gremlins/gremlins/issues/321)), so `Not viable` stays at zero and kill counts can run high. Work from survivors rather than from the score.
- `--diff` can silently skip every mutant in a repository with several Go modules ([#296](https://github.com/go-gremlins/gremlins/issues/296)). Check the mutant count before trusting it.
- Generics aren't supported ([#252](https://github.com/go-gremlins/gremlins/issues/252)).
- `--diff` still gathers coverage first, so a slow package costs its full test time before any mutant runs.
- It doesn't delete statements, so a removed call or missing state change goes unprobed.

Set no target score. A number invites weak tests written to reach it.

## Add statement deletion for critical packages

`go-mutesting` removes statements and expressions, which finds what Gremlins can't. Use Gremlins first, then this on the packages that matter most. Its tags aren't valid Go module versions, so install from `@latest`.

```sh
go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest
go-mutesting --disable 'numbers/*' ./internal/outcomes/...
jq -r '.escaped[] | "\(.mutator.mutatorName) \(.mutator.originalFilePath):\(.mutator.originalStartLine)"' report.json
```

| Tool, on one 960-line package | Mutants | Survivors | Time |
| --- | --- | --- | --- |
| Gremlins | 188 | 21 | 26 seconds |
| `go-mutesting --disable 'numbers/*'` | 417 | 114 | 1 minute 21 seconds |
| `go-mutesting` with every mutator | 543 | 186 | 3 minutes 46 seconds |

The extra survivors are worth having: deleting one `Mean` assignment failed no test, because no test checked `Mean` at all. The `numbers/*` mutators are worth disabling, since 72 of the 186 survivors only changed a numeric literal.

Three things catch people out. `PASS` in its output means the mutant was caught, and `FAIL` means it survived. It writes `report.json` to the working directory, 15MB here, so run it in a clone. Its own documentation warns about false positives, so check each survivor as described below.

## Read survivors as a map

A surviving mutant is a place to look, not a defect. Work through the survivors in the code that matters most:

1. **Rank by what the code does, not the mutation type.** A boundary survivor in a fee calculation matters more than an arithmetic survivor in a log line. Clusters in one function usually mean a path with no real assertions.
2. **Find out why it survived.** The code may never run, tests may run it without asserting the outcome, or an error path may have only happy-path tests.
3. **Reason about the original code,** not the mutant. A defect the mutation introduced says nothing about the real program.
4. **Try to construct the failing contract input.** Then classify it: a confirmed bug needing a contract-based regression, a weak test with correct code, dead code to remove, or an equivalent mutant. The verifier can reproduce a case; a fresh author owns behavioral test changes through the approved contract packet.

Boundary survivors are the most common useful finding. A survivor on `len(value) <= limit` usually means no test uses a value exactly at the limit and one past it. Establish the boundary from the contract, then ask the author to cover both through permitted feedback.

## Before calling a mutant equivalent

An equivalent mutant behaves identically, so no test can kill it. Don't use the label to dismiss a survivor. Check all of these:

1. The types involved, because `x > 0` and `x != 0` match for unsigned values and differ for signed ones.
2. That the code is reachable. Untested isn't unreachable.
3. That the changed value is actually read afterwards.
4. That behavior matches across the whole input domain, including boundaries and extremes.
5. That you tried and failed to construct an input telling them apart.

If a check stays uncertain, report the mutant as unresolved rather than equivalent.

_The survivor workflow and equivalence checks draw on ideas from `trailofbits/skills`, restated here for Gremlins._
