# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
| Original worker inputs remain complete | internal/cli/compact_selective_test.go | TestSelectiveCompactSavedWorkersRetainPolicyAndOriginalInstructions |
| Original worker inputs remain complete | internal/cli/compact_delivery_test.go | TestNativeDispatchMinimalReturnIsUsableForBothHosts |
| Quiet observations and existing history | internal/roles/coordinator_protocol_test.go | TestCoordinatorGuidedProtocol |
| Return claims match their evidence | internal/cli/compact_delivery_test.go | TestWorkerReturnSupportReadsAndRunnerOutcomesStayRejected |
| Complete transport accounting | internal/cli/compact_delivery_replay_test.go | TestCompactDeliveryOfflineReplay |

The [delivery report](../../../docs/benchmarks/compact-delivery-report.md)
separates scripted byte/call reductions from unknown provider usage and quality.
Existing lifecycle, failure, noisy-log/UTF-8 expansion, opt-in and original packet
regressions remain required. No paid model call or replacement pilot is part of
this validation.

17 November 2026: local Linux build, vet and full race-enabled suite passed.
golangci-lint 2.15.0 and govulncheck 1.9.0 passed for Linux, Darwin and Windows
targets. Generated help, Markdown/changelog checks and all OpenSpec records pass.
Native macOS/Windows runtime and CI outcomes are outside this local validation.
The delivery replay recorded 57 versus 48 calls with equal critical facts, four
checks and four finished intervals in each arm; provider usage/quality remain
unknown. The raw ledger sums reconcile; the published summary contains no host
paths. Historical experiment artifacts remain unchanged.
