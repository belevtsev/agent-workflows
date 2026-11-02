# Go fuzz targets and bounded campaigns

Read this for a Go parser, codec, normalization, or state invariant that benefits
from generated inputs. Follow repository Go pins, commands, build tags, platform,
and execution environment. An explicit no-tests instruction also forbids a fuzz
campaign; prepare the target or plan only when that is within scope. Use existing
tooling and dependencies, and report missing tools instead of installing them
automatically.
Use the [official Go fuzzing guide](https://go.dev/doc/security/fuzz/) for the
current runner and corpus mechanics rather than inventing version-specific flags.

The [test-author protocol](../../task-orchestration/references/test-author.md)
applies to targets, generators, harness helpers, and seed/corpus changes. The
author uses approved contracts, interface extracts, scaffolding, and dependencies;
the source-aware verifier owns integrated execution and raw failure evidence.
Missing bounds, behavior, or private seams go to the coordinator, not source search.

## Define the domain and an oracle

Name the input domain, externally relevant invariant, and failure signal before
writing `FuzzXxx`. Distinguish arbitrary malformed input from valid values and
document bounds that are part of the actual contract. A crash-only target can find
panics but cannot establish correct decoding, ownership, or authorization.

Use an approved independent specification, trusted reference, small model, or
justified contractual property. Examples include semantic encode/decode
preservation for valid messages,
normalization idempotence, an exact error contract for invalid input, or state
transitions preserving a named invariant. Protobuf serialization bytes are not a
canonical oracle: compare decoded meaning, presence, and relevant unknown fields.
Do not duplicate the implementation in the assertion or discard all inconvenient
inputs. Check positive, negative, and boundary cases so an always-rejecting decoder
or constant normalizer would fail the target.

If the target limits size, nesting, operations, or total work, derive those limits
from the supported contract and state the excluded domain. Avoid accidental
quadratic work or unbounded allocation in the harness itself. A claim about inputs
beyond the exercised limits remains unverified.

## Make seed evidence deterministic

Add small representative `f.Add` seeds: valid examples, empty/default values,
boundary lengths, malformed/truncated encodings, and known regressions. Seeds and
per-input assertions must reproduce without fresh random values, wall-clock
assumptions, or input-order dependence. Give each seed a meaningful intended case;
large copied corpora do not replace a useful oracle.

Ordinary `go test` executes the seed corpus as regression cases; it does not prove
that an exploratory fuzz campaign ran. Keep seed-only results, campaign results,
race checks, and integration evidence separate. Fuzzing a pure function does not
establish real filesystem, network, TLS, process, or database behavior.

## Isolate effects and bound the run

Keep each input independent and reset mutable state. Use temporary files, bounded
in-memory models, synthetic providers, and explicit cleanup. Do not contact live
services, issue credentials, operate production processes, or reuse shared mutable
fixtures. Background work must be joined or canceled before the input finishes;
parallel fuzz workers need isolated resources. Avoid per-input subprocesses or
other costly effects unless that real boundary is the purpose and is safely bounded.

The verifier chooses one target and a fixed time or iteration budget, worker
count, and deadline
consistent with the repository and available resources. An illustrative native Go
shape is `go test ./path -run '^$' -fuzz '^FuzzTarget$' -fuzztime=30s -parallel=2`;
derive the real package, target, flags, and required container from maintained
repository instructions. Do not turn a focused check into an unbounded campaign
or rerun matching evidence solely because the conversation advanced.

Fuzzing can write minimized failures under `testdata/fuzz/FuzzTarget` and entries
in the Go fuzz cache, even while testing source read-only. Treat corpus/cache
writes as execution effects: use the authorized isolated workspace and cache,
preserve a useful regression deliberately, and disclose writes when the task's
scope requires it. Do not run where no-tests or no-writes scope forbids them.

## Reproduce, minimize, and retain the regression

On failure, the verifier retains the exact input/corpus identity, failing
assertion or panic,
source SHA/diff, Go version, package, target, build tags, flags, environment, and
campaign budget. Record minimization or timeout status. An unreproduced timeout
is an observation, not proof of a semantic defect.

The verifier re-runs the exact failing corpus entry using the repository's
supported command, then minimizes while preserving the same failure signal and
permitted budget. It confirms the original code fails for the intended reason and the fix passes that
same input. Return only the case/requirement ID, contract input, expected/actual
outcomes, and test or approved-interface diagnostics to the author. Raw panics,
traces, coverage, and implementation details remain with the verifier. The author
retains or changes a minimized corpus case or focused named regression within the
approved contract and scope; the coordinator must not silently edit seeds or
assertions. Do not replace the regression with a broad happy-path test.
If a campaign is unavailable, report the seed-only or static evidence accurately
and give the bounded next check without claiming fuzz exploration passed.
