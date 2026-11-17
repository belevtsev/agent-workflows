# Verification

Ordinary validation uses local fixtures and fake executables and does not launch
a model. The separate report records actual offline receipts, canary status and
any authorized live results. A mapped test or planned cell alone proves no run.

| Scenario | Test file | Text |
| --- | --- | --- |
| Plain task start | internal/cli/compact_selective_test.go | TestSelectiveCompactTaskStartRequiresExplicitOptIn |
| Fresh selective worker | internal/cli/compact_selective_test.go | TestSelectiveCompactSavedWorkersRetainPolicyAndOriginalInstructions |
| Observed collector request errors | internal/cli/compact_request_regression_test.go | TestCompactCollectorRequestErrorsDoNotWriteAndTypedCommandsRecover |
| Evidence outside a preview | internal/cli/compact_replay_test.go | TestCompactSelectiveReplayRequiredFacts |
| Incomplete or failing v2 evidence | internal/pilot/quality_regression_test.go | TestCharsetRegressionV2RequiresBothFamiliesAndExecutedEvidence |
| Canary profile validation | internal/pilot/canary_budget_test.go | TestCanaryProfileDoesNotWeakenStudyValidation |
| Captured budget stop | internal/pilot/canary_budget_test.go | TestCanaryBudgetStopsLocalDescendantsAndFurtherLaunches |
| Incomplete planned cells | internal/pilot/canary_budget_test.go | TestCanaryLaunchReserveAndMissingResultFailClosed |
| Portable rollout paths | internal/pilot/harness_test.go | TestPilotRolloutPathRequiresCleanLocalPath |
| Trusted repository aliases | internal/pilot/grading_test.go | TestGradeCandidateResolvesAttestedRepositoryAliases |

Focused selective CLI and original-instruction regressions passed locally on
17 November 2026. Expanded replay and budget tests use fake model processes;
they are not paid execution. V2 source probes run only with explicit offline
paths and keep their observations in a new directory. The old reference is
rejected as v2 ground truth; the old study retains its original test verdicts.

The [selective report](../../../docs/benchmarks/selective-compact-report.md)
retains measurements and current execution status. Core build, vet, Linux race
and three-platform lint/vulnerability checks passed in the coordinator receipt
`20261117T052032Z-core-338302247`. The final source, including the Go-only
freeze helper, passed all twelve core steps again in
`20261117T052349Z-core-3192678699`, including the full Linux race suite (31.6 s).
Documentation and record validation are recorded after the final content check.

Focused Markdown-anchor, documentation-index and changelog checks passed. The
new record also passed `kitsu change check evaluate-selective-compaction`.
Whole-repository repocheck, the separate validation executable and all change
records passed in `20261117T052258Z-docs-3672219975` before source freeze.

A final review added 26 CLI request-error cases after the experimental source
freeze. These cover observed unknown-field aliases, malformed operations, IDs,
versions and input framing, with no state writes and successful typed recovery.
The focused test and CLI lint passed. This test-only addition and reporting
edits do not change any frozen production executable, harness or solver input.
The final full Linux race suite (34.7 s) and pinned lint for all three release
platforms then passed in `20261117T053357Z-all-448550227`.

The bounded live attempt stopped at 3,257,291 captured tokens. All four causal
sessions reconcile with the live ledger; the interrupted tail is unknown.
The candidate passed eighteen independent acceptance criteria, the original
suites and patch integrity. Three managed worker intervals finished, but no
Git worker, final commit or completed workflow exists. The other three cells
remain unstarted. This is not a completed on/off comparison or savings result.
Independent audit and all fourteen public canary evidence hashes were verified.
The original mechanical scan is retained with its three command-text false
positives explained; actual compact-check execution was zero. Native encrypted
message equality and full protocol compliance remain unknown.

PR #232 CI follow-up on 17 November 2026 corrects two native-platform issues:
Windows clean-path comparisons now normalize native separators, and trusted
candidate attestations resolve directory aliases before exact identity checks.
Traversal, relative/absolute-path restrictions, root containment and tree
bindings remain enforced. Local symlink fixtures reproduce the alias failure
without model execution; negative tests retain rejection of other repositories.

The Linux context test now checks status and length before indexing, preserving
an actionable error instead of panicking. The original empty-result condition
was not reproduced in 150 focused race runs and ten full context-package race
runs; this assertion change is diagnostic, not a claimed production fix.
The combined source passed build, vet, full Linux race tests and pinned
lint/vulnerability scans for Linux, Darwin and Windows in
`20261117T060557Z-core-3588456946`. The final independent outside-root assertion
also passed its focused race test after restoring a valid alias attestation.
