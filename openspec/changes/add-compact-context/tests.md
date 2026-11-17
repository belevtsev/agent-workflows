# Tests: add-compact-context

| Scenario | Test file | Text |
| --- | --- | --- |
| Collection retains ranges and deterministic search facts | internal/contextpack/context_test.go | TestCollectDeterministicRangesSearchAndPartialFailures |
| Executable or malformed requests are refused | internal/contextpack/context_test.go | TestContextRequestRejectsExecutableAndMalformedInput |
| Ignored or escaping sources expose no content | internal/contextpack/context_test.go | TestCollectConfinesSourcesAndRespectsIgnoreRules |
| Unsupported text and excessive results stay explicit | internal/contextpack/context_test.go | TestBinaryInvalidUTF8AndOversizedSourcesAreExplicit |
| Pages recover every retained UTF-8 fragment | internal/contextpack/context_test.go | TestBudgetPaginationAndUTF8PreserveEveryLine |
| Failed checks retain tail diagnostics and full expansion | internal/cli/compact_test.go | TestCompactCheckLogPaginationReconstructsOriginal |
| Content HEAD and ignore changes invalidate artifacts | internal/contextpack/context_test.go | TestCacheInvalidatesContentHEADAndSourcePolicy |
| Git normalization cannot hide a raw-source change | internal/contextpack/context_test.go | TestRawContentInvalidatesDespiteGitLineEndingNormalization |
| Cache eviction expiry and worktree isolation are observable | internal/contextpack/context_test.go | TestCacheEvictsOldestAndKeepsPrivateModes |
| Source changes during preparation prevent publication | internal/contextpack/context_test.go | TestSaveRejectsChangedSourceAfterPreparation |
| Brief preserves constraints and receipt ordering | internal/cli/compact_test.go | TestCompactBriefRetainsAuthorityAndReceiptBeforeBaseline |
| Invalid brief requests write no receipt | internal/cli/compact_test.go | TestCompactBriefValidatesBeforeWritingReceipts |
| External selected skills invalidate old briefs | internal/cli/compact_test.go | TestBriefExternalSkillChangesInvalidateOnlyItsPolicy |
| Compact checks preserve ordinary results | internal/cli/compact_test.go | TestCompactChecksPreserveOutcomesLogsAndEvidence |
| Forced base-implied runs keep executed logs | internal/cli/compact_test.go | TestForcedBaseImpliedFailureRetainsDiagnostics |
| Large retained logs preserve check exits | internal/cli/compact_test.go | TestCompactMultipleLargeLogsDoNotReplaceCheckExit |
| New evidence or expiry invalidates a verdict | internal/cli/compact_test.go | TestCompactReportRejectsEvidenceCompletionAfterVerdict |
| Compact commands coexist with hook ingress | internal/cli/compact_test.go | TestCompactCLIHelpCompletionAndLegacyHookStayIndependent |
| Compact task handoff remains opt-in | internal/cli/compact_test.go | TestTaskStartCompactModeIsExplicitAndHandoffOnly |
| Offline comparisons preserve required facts | internal/cli/compact_benchmark_test.go | TestCompactWorkflowNeededFacts |

Companion coverage includes TestCacheExpirationCorruptionAndMissingArtifacts,
TestLinkedWorktreesDoNotShareContextArtifacts and
TestCompactReportKeepsExpiryEvenWhenItPassesDuringPreparation. Cancellation, normal skip
reuse, declaration order and exact reconstructed log coordinates have additional regression
coverage. TestCompactWorkflowNoisyCheckLogRecoverable verifies that optional full-log
retrieval is counted as an extra observation rather than a free operation.
TestRawDigestFramesBinaryContentWithoutBoundaryAmbiguity verifies that a clean filter cannot
hide a text-to-binary mutation through ambiguous file-boundary framing.
TestCacheRemovesOrphanArtifactTemporariesUnderWriterLock verifies cleanup after a killed writer
without actually terminating a test process. TestCompactCheckCancellationMatchesNormalRunner
checks the same interrupted outcome and exit in compact and normal modes.

## Validation

On 16 November 2026 these commands passed on Linux:

```sh
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -race -count=1 ./...
GOWORK=off golangci-lint run ./...
GOWORK=off govulncheck ./...
GOWORK=off go test ./internal/cli -run '^TestCompactWorkflow' -count=1
GOWORK=off go test ./internal/cli -run '^$' -bench '^BenchmarkCompactWorkflows$' -benchtime=1x -count=5
```

All 39 tested packages/scenario suites passed (two packages report no test files). The CLI's
race run includes cancellation equivalence, large-log reporting and log-coordinate reconstruction.
Offline comparisons counted all needed expansion requests and response bytes; they show
increases for full-evidence exploration and handoff JSON as well as compact noisy-check output.
See the [raw samples](../../../docs/benchmarks/compact-context.txt) and
[interpretation](../../../docs/compact-context.md#observed-offline-comparison).

Both changed diagrams passed archify showcase validation and headless browser checks. Actual
SVG Auto exports were inspected in light and dark themes; widths are 788 and 832 pixels.
The generated role/plugin file and command reference were regenerated and checked.

Lint and vulnerability checks also passed with GOOS=darwin and GOOS=windows. Both CLI and
contextpack test binaries cross-compiled for each target. The final CLI race regression
subset passed after output/reporting refinements. Native CI already defines Linux/macOS/Windows
runs; cross-compiling locally does not demonstrate
native runtime behavior for this change. No paid A/B host session was run, and host token
accounting/unknown values remain unchanged. Formal review and human approval tables stay empty.
