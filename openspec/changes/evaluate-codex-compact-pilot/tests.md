# Verification

The model pilot is opt-in. Ordinary unit tests use fake local processes and do
not invoke a model. Live results are recorded separately from these harness
checks in the [pilot report](../../../docs/benchmarks/codex-compact-pilot-report.md).

| Scenario | Test file | Text |
| --- | --- | --- |
| Ordinary tests | internal/pilot/pilot_test.go | TestCodexPilot |
| Changed source or prompt | internal/pilot/harness_test.go | TestPilotFreezeDetectsDriftAndPreservesManifest |
| User changes the budget | internal/pilot/recovery_test.go | TestPilotRecoveryExportsPartialCandidateAndGradesOffline |
| Candidate changes its own tests | internal/pilot/grading_test.go | TestGradeOracleIgnoresCandidateTestAndBuildControlChanges |
| Blind review runtime | internal/pilot/review_run_test.go | TestPilotReviewRuntimeStaysReadonlyAndBlind |
| Repeated usage event | internal/pilot/harness_test.go | TestPilotDuplicateTraceAliasesAreCountedOnce |
| Missing worker rollout | internal/pilot/usage_regression_test.go | TestPilotMissingWorkerUsageRemainsUnknown |
| Forced stop during a request | internal/pilot/analysis_test.go | TestPilotAnalysisInterruptedStreamHasUnknownTailUsage |

Additional checks cover paired scheduling and controller assignment, rejected
calibration, snapshot history, remote solution fetches, unsafe archive paths,
required acceptance execution and precedence of known protocol violations.

Live replacement execution: 24/24 retained attempts, 108 actual Sol/high
sessions, 24/24 frozen acceptance/regression/infrastructure passes; eight host
exits and sixteen cutoffs. Independent protocol audit observes required
mechanics in 9/12 normal and 1/12 compact attempts, but certifies no full
protocol payload. Blind review: 21 valid and three timeout/unknown results.
Offline HTTP probes confirm additional charset defects in both arms, without
altering frozen test scores. Captured solver usage is at least 61,681,571;
no pair has complete accounting. See the report and its sanitized exports.

Final integration passed on 16 November 2026:

- `go build ./...` and `go vet ./...`.
- `go test -race -count=1 ./...` on Linux, including the fake-process harness,
  protocol/accounting, budget-profile, recovery, blind namespace and archive tests.
- `golangci-lint run ./...` and `govulncheck ./...` for Linux, Darwin and Windows.
- Whole-repository repocheck and `kitsu change check` with a separately built executable.

Receipts are under `~/.local/state/kitsu/pilots/codex-compact-high-20261116/validation/`:
`20261116T205431Z-core-2632172735` (build/vet),
`20261116T205630Z-core-2814207726` (three-target scans),
`20261116T210013Z-core-602627045` (final full race), and
`20261116T210045Z-docs-1946280261` (docs/record).

Earlier failures are retained. Full race initially hit a nondeterministic
stdout/stderr fixture order and `/tmp` quota exhaustion. The fixture's final
diagnostic now shares one stream with its preceding output and passed 30 race
repetitions; disk-backed temporary storage resolved environmental failures.
Native macOS/Windows runtime execution is unavailable here.
