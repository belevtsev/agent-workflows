# Verification

All scenario mappings, final documentation/record checks and bounded fuzz
campaigns passed locally. No model, pilot or paid
probe is part of this validation.

| Scenario | Test file | Text |
| --- | --- | --- |
| Preserve metadata-free behavior | internal/cli/task_verification_state_test.go | TestTaskVerificationReadinessRequirements |
| Separate scheduling from dependencies | internal/decl/metadata_test.go | TestVerificationMetadata |
| Separate scheduling from dependencies | internal/cli/check_plan_test.go | TestAddVerificationMetadata |
| Separate scheduling from dependencies | internal/verification/plan_test.go | TestHistoryExclusionsNeverChangeEvidenceDependencies |
| Plan staged unstaged untracked and deleted paths | internal/verification/plan_test.go | TestPlanUsesCompleteSnapshotAndRetainsSuite |
| Plan staged unstaged untracked and deleted paths | internal/cli/check_plan_test.go | TestCheckPlanJSONAndHumanOutput |
| Keep unmapped full checks | internal/verification/plan_test.go | TestPlanUsesCompleteSnapshotAndRetainsSuite |
| Schedule original bytes hidden by normalization | internal/verification/raw_test.go | TestPlanSchedulesOriginalBytesHiddenByCleanFilter |
| Schedule original bytes hidden by normalization | internal/verification/raw_test.go | TestPlanSchedulesCRLFNormalizedOriginalBytes |
| Schedule original bytes hidden by normalization | internal/verification/raw_test.go | TestRawDifferencesSHA256Objects |
| Refuse unreadable original source | internal/verification/raw_test.go | TestRawSourceReadFailsClosed |
| Require and pin the first base | internal/cli/task_verification_application_test.go | TestTaskVerificationApplicationRequiresBase |
| Require and pin the first base | internal/verification/plan_test.go | TestPlanAlwaysVerifyUnbornAndPinnedBase |
| Refuse active managed intervals | internal/cli/task_verification_state_test.go | TestTaskVerificationActiveInterval |
| Stop on targeted failure | internal/cli/task_verification_application_test.go | TestTaskVerificationApplicationCalibration |
| Publish before final checks | internal/cli/task_verification_state_test.go | TestTaskVerificationPublicationOrdering |
| Publish before final checks | internal/cli/task_verification_state_test.go | TestTaskVerificationPublicationRejectsSourceDrift |
| Reuse unchanged successful verification | internal/cli/task_verification_application_test.go | TestTaskVerificationApplicationCompactEquivalence |
| Reuse unchanged successful verification | internal/verification/history_test.go | TestHistoryPublishPreservesAttemptsAndIdempotence |
| Reject missing stale or unavailable local evidence | internal/cli/task_verification_state_test.go | TestTaskVerificationFreshness |
| Reject missing stale or unavailable local evidence | internal/cli/task_verification_state_test.go | TestTaskVerificationCloneAndCorruption |
| Reject missing stale or unavailable local evidence | internal/cli/task_verification_application_test.go | TestTaskVerificationApplicationRejectsEditedGateLogs |
| Reject missing stale or unavailable local evidence | internal/cli/task_verification_state_test.go | TestTaskVerificationMissingFullCheckLog |
| Establish the first local behavioral observation | internal/cli/task_verification_state_test.go | TestTaskVerificationFreshness |
| Reobserve changed raw source or deciding evidence | internal/cli/task_verification_state_test.go | TestTaskVerificationMixedHiddenSourceChange |
| Reobserve changed raw source or deciding evidence | internal/cli/task_verification_state_test.go | TestTaskVerificationOriginalSkillFreshness |
| Reobserve changed raw source or deciding evidence | internal/cli/task_verification_state_test.go | TestTaskVerificationRecordReplacement |
| Validate history without private logs | internal/cli/task_verification_state_test.go | TestTaskVerificationCloneAndCorruption |
| Preserve full small and compact authority | internal/cli/task_verification_application_test.go | TestTaskVerificationApplicationCompactEquivalence |
| Preserve full small and compact authority | internal/cli/task_verification_state_test.go | TestTaskVerificationReadinessRequirements |
| Preserve full small and compact authority | internal/cli/task_verification_state_test.go | TestTaskVerificationSnapshotDelta |

## Changed tests

- `internal/repocheck/size_test.go`: give the new verification subsystem its own
  bounded 2,200-line budget while preserving the original gate and other subsystem
  limits (Coordinate source-bound attempts; Derive current readiness from local evidence).

## Validation

Final documentation/record checks and bounded fuzz campaigns passed. The
[guide](../../../docs/verification.md) explains runtime behavior and keeps
historical offline calibration separate from task verification.

Passed local checks:

```sh
GOWORK=off go test ./internal/decl ./internal/verification \
  -run '^(TestVerificationMetadata.*|TestPlan.*|TestHistory.*)$' -count=1
GOWORK=off go test ./internal/verification -count=1
GOWORK=off go test ./internal/cli -run '^TestTaskVerificationApplication' -count=1 -v
GOWORK=off go test ./internal/cli \
  -run '^TestTaskVerification(Freshness|OriginalSkillFreshness|RecordReplacement|MixedHiddenSourceChange|CloneAndCorruption|SnapshotDelta|ActiveInterval|ReadinessRequirements)$' -count=1 -v
GOWORK=off go test ./internal/repocheck ./internal/roles -count=1
kitsu change check
```

The final focused runtime run also passed all `TestTaskVerification*` cases,
including publication ordering and missing remaining-check log recovery.
The publication child reads actual immutable history before its final check;
its recorded tree contains the binding and equals the later staged tree.
Freshness cases cover raw/visible changes, selected original skill sources,
replaced deciding records, missing/edited logs and absent/corrupt private proof.
An actual clone starts with zero local run records, reports unavailable while
structural history validation passes, then executes its own two local checks.

Review identified a publication race between the initial observations and
shared-summary publication. `TestTaskVerificationPublicationRejectsSourceDrift`
uses actual Git snapshot/raw-byte maps to allow only this task's owned summary
write while refusing visible/hidden source edits and another task's history.
All full-output formats repeat stable source/raw/skill/evidence observations
after evidence hashing before reporting readiness. Final build, vet, the full
Linux race suite and Linux/Darwin/Windows lint and vulnerability checks passed
after these guards were added.

Final repository gates passed after the source-publication and report-observation
guards, including their regression tests:

```sh
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -race -count=1 ./...
GOWORK=off golangci-lint run ./...
GOWORK=off GOOS=darwin golangci-lint run ./...
GOWORK=off GOOS=windows golangci-lint run ./...
GOWORK=off govulncheck ./...
GOWORK=off GOOS=darwin govulncheck ./...
GOWORK=off GOOS=windows govulncheck ./...
```

Build, vet and race tests executed on Linux. The Darwin/Windows lint and
vulnerability checks used target-platform analysis; no native Darwin/Windows
test execution is claimed.

Bounded campaigns used two workers for five seconds per target:

```sh
GOWORK=off GOMAXPROCS=2 go test ./internal/contextpack -run '^$' \
  -fuzz '^FuzzContextReadSearchEquivalence$' -fuzztime=5s -parallel=2
GOWORK=off GOMAXPROCS=2 go test ./internal/contextpack -run '^$' \
  -fuzz '^FuzzContextCursorValidation$' -fuzztime=5s -parallel=2
GOWORK=off GOMAXPROCS=2 go test ./internal/cli -run '^$' \
  -fuzz '^FuzzWorkerReturnClaimPreservation$' -fuzztime=5s -parallel=2
```

| Target | Executions | Result |
| --- | ---: | --- |
| FuzzContextReadSearchEquivalence | 769 | pass |
| FuzzContextCursorValidation | 80,942 | pass |
| FuzzWorkerReturnClaimPreservation | 33,854 | pass |

Each completed in about five to six seconds of wall time and emitted no failure
corpus. These finite execution counts establish only that no failure was found
in the exercised inputs; they imply no coverage or general-quality result.

A separate application race run passed, including edited-log refusal for both
required and remaining full checks:

```sh
GOWORK=off go test -race ./internal/cli -run '^TestTaskVerificationApplication' -count=1
```

Fresh JSON log digests match actual retained bytes; ordinary verification must
reobserve edited logs rather than expanding them as current evidence.

The application calibration executes a declared child command which owns its
prerequisite check, loopback HTTP application startup, assertions and cleanup;
Kitsu manages none of that service lifecycle. Three known defects are detected:
empty HTTP values accepted, incompatible HTTP response version and incompatible
saved-format version. A separate missing-prerequisite case fails closed. Five
original/corrected controls pass, with no false green in these finite cases.
Targeted failures execute one check and stop before the full check; the
saved-format failure executes both checks. Corrections establish real passing
behavior, and unchanged successful verification adds no run or history.

The compact fixture recovers complete assertion logs and compares JSON/compact
facts within a 4,096-byte budget. The latest focused run
observed 2,459 JSON bytes and 3,154 compact bytes; timestamps can vary the wrapper
by a byte. The wrapper is larger for this small fixture. These measure
serialized output bytes, not provider tokens or savings. Finite fixture
results establish neither general application correctness nor model-review
quality or general compact/ordinary quality equivalence.

Affected `task-workflow` and `overview` diagrams passed archify showcase
finalization with validate/deliver/check/browser-check all passing. Canonical
browser SVG Auto exports were inspected in light/dark themes at 816 and 832
pixels wide. No SVG geometry or content was edited by hand.
