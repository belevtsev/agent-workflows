# Verification

Captured-evidence diagnostics, optional notes, repeated failure groups, nested
CLI help/completion and budget CLI/package tests passed locally. Required
repository validation passed on 2026-11-17. No paid model, pilot or probe is
part of this change; historical experiment artifacts remain frozen.

| Scenario | Test file | Text |
| --- | --- | --- |
| Preserve ordinary usage output | internal/cli/usage_insights_test.go | TestUsageInsightsPreserveOrdinaryOutputAndCreateNoWork |
| Keep missing shared and unmanaged attribution unknown | internal/cli/usage_insights_test.go | TestUsageInsightsWithoutWorktreeRetainsCapturedEvidence |
| Retain exact captured accounting | internal/insights/diagnostics_test.go | TestDiagnosticsReconcilesCapturedAccountingAndCacheSubsets |
| Retain exact captured accounting | internal/insights/diagnostics_test.go | TestDiagnosticsMalformedAndConflictingCountersStayUnknown |
| Keep missing shared and unmanaged attribution unknown | internal/insights/diagnostics_test.go | TestDiagnosticsUnknownAndSharedUsageNeverAssigned |
| Keep missing shared and unmanaged attribution unknown | internal/insights/diagnostics_test.go | TestDiagnosticsCheckRunsRequireExplicitTaskReferences |
| Union overlapping prepared-to-finished spans | internal/insights/diagnostics_test.go | TestDiagnosticsManagedIntervalsUseUnionAndPreserveUnknowns |
| Preserve gaps and timing interpretation | internal/insights/diagnostics_test.go | TestDiagnosticsManagedIntervalsUseUnionAndPreserveUnknowns |
| Project consequential claims and decisions | internal/insights/notes_test.go | TestOptionalWorkflowNotesPreserveClaimsAndOriginalReferences |
| Project consequential claims and decisions | internal/cli/usage_insights_test.go | TestUsageInsightsOptionalNotesAndCompactRecovery |
| Preserve prose partial formats and bounded omissions | internal/insights/notes_test.go | TestOptionalWorkflowNotesBoundOmissionsAndUnknownFormats |
| Group repeated failures with matching identities | internal/insights/recurrence_test.go | TestWorkflowFailureRecurrencesSeparateInputsOutcomesAndDuplicates |
| Group repeated failures with matching identities | internal/cli/usage_insights_test.go | TestUsageInsightsPreserveOrdinaryOutputAndCreateNoWork |
| Split changed content and recorded policy | internal/insights/recurrence_test.go | TestWorkflowFailureRecurrencesSeparateInputsOutcomesAndDuplicates |
| Preserve unconfirmed and missing troubleshooting evidence | internal/insights/recurrence_test.go | TestWorkflowFailureRecurrencesRespectHypothesisEvidence |
| Preserve unconfirmed and missing troubleshooting evidence | internal/insights/recurrence_test.go | TestWorkflowFailureRecurrencesBoundReferences |
| Preserve unconfirmed and missing troubleshooting evidence | internal/insights/recurrence_test.go | TestWorkflowFailureRecurrencesShowLaterPassingEvidence |
| Store separate full and small unit budgets | internal/taskbudget/budget_test.go | TestOptionalLoadAndPromotion |
| Store separate full and small unit budgets | internal/taskbudget/budget_test.go | TestSetPreservesOtherUnitBytesAndUnspecifiedFields |
| Store separate full and small unit budgets | internal/cli/usage_budget_test.go | TestUsageBudgetCLIRoundTripWithoutInstrumentationOrExecutions |
| Refuse invalid or unsourced metadata | internal/taskbudget/budget_test.go | TestInvalidUpdatesDoNotWrite |
| Refuse invalid or unsourced metadata | internal/cli/usage_budget_test.go | TestUsageBudgetFlagsRequireIndependentExplicitDeclarations |
| Show and reset one unit | internal/taskbudget/budget_test.go | TestSetPreservesOtherUnitBytesAndUnspecifiedFields |
| Show and reset one unit | internal/cli/usage_budget_test.go | TestUsageBudgetCLIRoundTripWithoutInstrumentationOrExecutions |
| Keep incomplete actuals from claiming under budget | internal/taskbudget/budget_test.go | TestCompareRetainsIncompleteAndUnknownActuals |
| Keep incomplete actuals from claiming under budget | internal/cli/usage_budget_test.go | TestUsageBudgetPlainDisplayPreservesPartialUsageAndSources |
| Report known captured exceedance | internal/taskbudget/budget_test.go | TestCompareRetainsIncompleteAndUnknownActuals |
| Report known captured exceedance | internal/cli/usage_insights_test.go | TestUsageInsightsOverflowPreservesMeasuredLowerBounds |
| Keep incomplete actuals from claiming under budget | internal/cli/usage_insights_test.go | TestUsageInsightsBudgetActualsKeepScopeAndCompleteness |
| Retain existing workflow and source authority | internal/cli/usage_insights_test.go | TestUsageInsightsOptionalNotesAndCompactRecovery |
| Retain existing workflow and source authority | internal/cli/usage_budget_test.go | TestUsageBudgetCLIRoundTripWithoutInstrumentationOrExecutions |
| Retain existing workflow and source authority | internal/cli/usage_insights_test.go | TestUsageInsightsNestedHelpAndCompletion |

## Changed tests

- `internal/repocheck/size_test.go`: give diagnostics, optional notes, budget
  metadata and their `usage_*.go` adapters a separate bounded 3,000-line category.
  All existing subsystem caps remain unchanged (Inspect captured workflow
  evidence; Project optional consequential notes; Record estimates and sourced
  declared limits).
- `internal/cli/ux_help_test.go`: exercise each command's actual parent path
  when inserting `help`, including new three-level budget commands. Existing
  no-runtime/no-write help expectations remain unchanged (Preserve existing
  workflow authority).
- `internal/cli/usage_diagnostics_test.go`: snapshot metadata from open handles
  after native Windows CI exposed stale directory-entry timestamps. Preserve
  mode, modification-time and content comparisons; a control test detects
  content-only changes, timestamp-only changes and new lock files (Inspect
  captured workflow evidence).

Existing runner, token attribution and frozen experiment expectations retain
their original scope.

## Validation

Passed the captured-evidence diagnostics suite and vet checks:

```sh
GOWORK=off go test -race ./internal/insights ./internal/taskbudget ./internal/cli \
  -run '^(TestDiagnostics|TestUsageDiagnostics|TestOptionalWorkflowNotes|TestWorkflowFailureRecurrences|TestUsageInsights|TestUsageBudget|TestUXHelpAtEveryDepth|TestUXCommandReferenceIsGenerated)' -count=1
GOWORK=off go vet ./internal/insights ./internal/cli
GOWORK=off go test -race ./internal/taskbudget ./internal/cli \
  -run '^(TestUsageBudget|TestCompare|TestSet|TestStrictRecord|TestOptionalLoad|TestUnsafeAndBounded|TestConcurrentUnit|TestInvalidUpdates|ExampleCompare)' -count=1
GOWORK=off go test ./internal/roles -count=1
GOWORK=off go test ./internal/cli -run '^TestUXCommandReferenceIsGenerated$' \
  -update-cli-reference -count=1
GOWORK=off go test ./internal/repocheck \
  -run '^(TestRelativeMarkdownLinksResolve|TestMarkdownAnchorsResolve|TestTheIndexNamesEveryDocument|TestChangelogLineLength|TestDocumentsStateNoBenefitWithoutTheComparison|TestEveryDiagramHasItsSpecItsExportAndAPlace)$' -count=1
GOWORK=off go run ./cmd/kitsu change check add-workflow-insights
```

`TestUsageDiagnosticsRepeatedReadsCreateNoLedgerLocksChecksOrHostWork`,
`TestUsageDiagnosticsReadsOriginalStageReceiptsWithoutMutation` and
`TestUsageDiagnosticsRefusesUnsafeAndDamagedRecords` also passed in
`internal/cli/usage_diagnostics_test.go`. These cases verify read-only joins,
original receipts and unsafe/damaged records; they do not validate recurrence
logic or certify authored notes.

Budget package and CLI tests passed with the race detector, including strict
record validation, independent unit preservation, safe full/small storage,
concurrent updates and invalid-update refusal. The complete CLI roundtrip
exercises small-budget set/show/list/task-show/task-check/reset, both units,
field and sibling preservation, and an existing full folder with absent
metadata. It observes no private execution state or model invocation and
preserves unknown actuals. Plain rendering also quotes authored terminal
controls while JSON preserves original strings.

Optional note and recurrence tests preserve original references and claim
authority, bound omitted rows/cells/references, retain unknown formats and
separate failed/timed-out outcomes, duplicate records, content and policy.
The integration tests preserve ordinary usage output, create no new work,
recover exact original note bytes through compact expansion and preserve
budget actual completeness and managed-time lower bounds.

Nested help/completion tests also passed after correcting the fixture to use
the existing completion callback protocol. Newer passing evidence remains
visible beside historical recurrence hints. Overflow cases retain a
representable known measured lower bound while marking completeness false,
so a proven exceedance is not lost.

The final repository checks passed after correcting an adviser-record exit-code
regression caught by the existing `TestSmallTaskRecordChecks` scenarios. Malformed
small histories continue through record validation and return content-invalid
exit code 5; checking them invokes no adviser. The new
`TestUsageInsightsWithoutWorktreeRetainsCapturedEvidence` also preserves captured
usage without trying to open optional worktree-only notes or budget metadata.

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

The tools match the versions pinned in `CONTRIBUTING.md`: golangci-lint v2.15.0
and govulncheck v1.9.0. Each lint target reported zero issues; each vulnerability
target reported no vulnerabilities. Runtime tests ran on Linux. Darwin and
Windows checks were static target analysis, not native runtime validation.

Tests use local captured fixtures and project commands only. Interval assertions concern
managed observation spans, not real worker runtime. Passing projection tests
do not certify authored sources or confirm hypotheses.

## Separately authorized paid follow-up

The owner subsequently approved two Sol/high Codex attempts on the same real
pflag task, twelve minutes each, with a new 10M captured-token envelope. Both
attempts completed; complete native accounting reconciles to 3,622,559 tokens.
The [follow-up report](../../../docs/benchmarks/workflow-insights-paid-report.md)
records 1,638,219 ordinary versus 1,984,340 compact tokens (+21.1%), with both
passing protected acceptance, original regressions and patch integrity. One
pair does not establish general quality equivalence or savings. This paid run
has its own approval and budget; the implementation checks above invoked no
model. Compact remains opt-in.

`TestFocusedSourceFreezeRetainsUntrackedPackagesAndOriginalBytes` verifies
complete immutable source capture, including new untracked packages, rather
than relying on a tracked-only diff. `TestFocusedDiagnosticReplayRemovesModelAndTunnel`
verifies that posthoc CLI replay removes the native model command and API
tunnel. Both are ordinary no-model regression tests in `internal/pilot`.

Explicit offline entries `TestFocusedCompletedRunOffline`,
`TestFocusedExportOffline`, `TestPilotCollectProtocolFacts` and
`TestFocusedInsightsOffline` passed against the completed evidence. Frozen CLI
diagnostics found four closed compact intervals and four associated checks.
Absent Kitsu token-hook records remain unknown; the native harness counters
are separate. Optional authored notes and budget metadata were not selected by
this small-path task. Prior experiment artifacts and measurements remain intact.
