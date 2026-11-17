# Verification

The [offline guide](../../../docs/verification.md) gives exact deterministic
tests, bounded fuzz commands and failing-corpus replay. These use retained
public evidence, generated fixtures, fake checks and local Git repositories;
they invoke no model and launch no paid pilot. A scenario mapping is not a run
receipt; executed validation is recorded below.

| Scenario | Test file | Text |
| --- | --- | --- |
| Replay labeled historical behaviors | internal/pilot/verifier_calibration_test.go | TestVerifierCalibration |
| Reject unusable calibration evidence | internal/pilot/verifier_calibration_test.go | TestVerifierCalibrationFailsClosed |
| Reject unusable calibration evidence | internal/pilot/verifier_calibration_test.go | TestVerifierEvidenceRejectsIncompleteJSON |
| Recover generated read and search evidence | internal/contextpack/equivalence_test.go | TestContextOfflineReadSearchEquivalence |
| Recover generated read and search evidence | internal/contextpack/equivalence_test.go | FuzzContextReadSearchEquivalence |
| Refresh changed source evidence | internal/contextpack/equivalence_test.go | TestContextOfflineFreshnessEquivalence |
| Preserve staged Git changes and state identity | internal/contextpack/equivalence_test.go | TestContextOfflineGitEquivalence |
| Reject invalid context continuation or metadata budget | internal/contextpack/equivalence_test.go | TestContextRequiredMetadataBudgetRefusal |
| Reject invalid context continuation or metadata budget | internal/contextpack/equivalence_test.go | TestContextCursorRejectsCrossItemAndMalformed |
| Reject invalid context continuation or metadata budget | internal/contextpack/equivalence_test.go | FuzzContextCursorValidation |
| Preserve worker claims without granting authority | internal/cli/verifier_equivalence_test.go | FuzzWorkerReturnClaimPreservation |
| Reconstruct coordinator transitions including clearing | internal/cli/verifier_equivalence_test.go | TestOfflineCoordinatorDeltaEquivalenceMatrix |
| Write a separate calibration receipt | internal/pilot/verifier_calibration_test.go | TestPilotVerifierCalibration |
| Write a separate calibration receipt | internal/pilot/verifier_calibration_test.go | TestVerifierCalibrationReceiptRefusesOverwrite |
| Preserve ordinary task starts and original worker inputs | internal/cli/compact_selective_test.go | TestSelectiveCompactTaskStartRequiresExplicitOptIn |
| Preserve ordinary task starts and original worker inputs | internal/cli/compact_selective_test.go | TestSelectiveCompactSavedWorkersRetainPolicyAndOriginalInstructions |

## Calibration interpretation

The passing calibration replay has 44 individual behavior observations: development uses
baseline/reference (four defects, 18 controls); validation uses
candidate-1/candidate-2 (eight defects, 14 controls). Expanded checks are
confirmed to detect all 12 labeled defects, with 32 accepted correct behavior controls,
zero false alarms and zero unknowns on the retained valid evidence. Negative
cases separately require unknown or rejection for unusable evidence. The
original-coverage replay identifies 11 labeled defects beyond original
task/control coverage; it does not execute or revise the old frozen grader.

This is public retrospective same-task evidence with overlapping behaviors;
candidate-1 is identical to the reference. The controls are individually correct
observations, not fully correct programs. Counts do not establish unseen
holdout performance, general model-review quality or workflow quality equivalence.

## Executed validation

Local validation on 2026-11-17 passed:

- `GOWORK=off go build ./...` and `GOWORK=off go vet ./...`.
- `GOWORK=off go test -race -count=1 ./...`, including calibration, negative
  evidence and receipt checks, generated context cases, both host state
  machines, and every deterministic fuzz seed.
- `golangci-lint run ./...` at v2.15.0 and `govulncheck ./...` at v1.9.0,
  with `GOWORK=off` for Linux, `GOOS=darwin` and `GOOS=windows`: no lint
  issues or reachable vulnerabilities reported.
- Markdown/changelog checks in `internal/repocheck`, `git diff --check` and
  `kitsu change check` for all OpenSpec records.
- Independent Sol/high subagent reviews of calibration, context oracles,
  protocol/state assertions and documentation; reported gaps were addressed.

The final bounded native Go fuzz campaigns used `GOMAXPROCS=2`, `-parallel=2`
and `-fuzztime=5s`, with the exact target commands in the guide:

| Target | Executions reported | Outcome |
| --- | ---: | --- |
| FuzzContextReadSearchEquivalence | 1,529 | passed |
| FuzzContextCursorValidation | 57,010 | passed |
| FuzzWorkerReturnClaimPreservation | 30,677 | passed |

These are exercised-input counts, not independent defect samples, exhaustive
coverage or a workflow quality estimate. No new failing corpus entry or
production defect was found. The optional receipt was emitted to a new local
file, independently of frozen historical artifacts; its SHA-256 is
`c7ed6ecf3cc2f71fbc71079b483edd7590797c7dd03c607ae5d51409a1f0db98`.

Native macOS and Windows runtime behavior requires those platforms;
cross-target static checks do not supply it. Historical paid comparison data
and reported results remain unchanged. No native model/accounting calibration
or further paid run was part of this validation.
