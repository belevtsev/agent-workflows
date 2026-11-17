# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
| Offline preparation and eligibility | internal/pilot/focused_prepare_test.go | TestFocusedPrepareOffline |
| Final tail and missing worker | internal/pilot/focused_report_test.go | TestFocusedSessionFinalTailRemainsUnknown |
| Final tail and missing worker | internal/pilot/usage_regression_test.go | TestPilotMissingWorkerUsageRemainsUnknown |
| Sanitized paired evidence | internal/pilot/focused_report_test.go | TestFocusedCompletedRunOffline |
| Sanitized paired evidence | internal/pilot/focused_report_test.go | TestFocusedExportOffline |

The [completed report](../../../docs/benchmarks/compact-delivery-paid-report.md)
records both passing independent acceptance/original-regression/integrity checks
and all ten terminal Sol/high sessions. Ordinary used 1,982,129 tokens; compact
used 1,872,131. The shared ledger reconciles to 3,854,260. Uncached input increased
18.7%; this is not a billing or general quality-equivalence claim. Actual code,
role/Git artifacts and original packets passed the separate read-only inspection.

Historical studies and frozen measured assets remain unchanged. No additional
model attempt, paid probe, extra judge or CI wait is part of final validation.

Final local checks passed on 17 November 2026: Linux build, vet and the full
race-enabled suite; golangci-lint 2.15.0 and govulncheck 1.9.0 for Linux,
Darwin and Windows targets; all OpenSpec records; generated help and Markdown/
changelog checks; and all four published artifact hashes. The README wording
was scoped to measured counts before the final successful full-suite rerun.
Native macOS/Windows runtime and CI outcomes are not claimed. Frozen production,
runner and production/guide source-patch hashes still match their approval.
