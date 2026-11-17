# Verification

All four improvements are implemented and verified offline on 17 November 2026
using Go 1.27.4. Implementation checks invoked no paid model pilot or probe.
A later owner-requested [paid pair](../../../docs/benchmarks/response-cycles-paid-report.md)
records actual usage and protected checks separately. Earlier frozen artifacts
remain unchanged. Offline replay metrics do not establish provider usage or quality.

| Scenario | Test file | Text |
| --- | --- | --- |
| Fit complete failed logs to the exact response budget | internal/contextpack/check_fit_test.go | TestCompleteFailedChecksFitExactSerializedBudget |
| Fit complete failed logs to the exact response budget | internal/cli/compact_logs_fit_test.go | TestCompactFittingFailuresAreCompleteAndExpansionNeverReruns |
| Share the budget and account for escaping | internal/contextpack/check_fit_test.go | TestFailedCheckFitUsesEscapedJSONAndSharedBudget |
| Share the budget and account for escaping | internal/cli/compact_logs_fit_test.go | TestCompactMultipleFailuresShareOneInitialBudget |
| Expand genuine noisy evidence without rerunning checks | internal/contextpack/context_test.go | TestCheckPreviewsRetainTailAndExpandFromOriginalLog |
| Run current recipes and retain the complete check suite | internal/cli/worker_hints_test.go | TestWorkerHintsRecipesUseCurrentCLIAndDeclaredChecks |
| Distinguish root and subdirectory lookup environments | internal/cli/worker_hints_test.go | TestWorkerHintsLookupScopesActualDirectoryWithRelativePATH |
| Distinguish root and subdirectory lookup environments | internal/cli/worker_hints_test.go | TestWorkerHintsUnknownDirectoryDoesNotPublishAvailability |
| Verify immutable original worker inputs | internal/cli/worker_hints_test.go | TestWorkerHintsSnapshotPreservesOriginalPacketAndFreshness |
| Complete original lifecycle with lean cadence | internal/cli/compact_cadence_replay_test.go | TestCompactCadenceLifecycleEquivalence |
| Retain actual failures at handoff boundaries | internal/cli/compact_cadence_replay_test.go | TestCompactCadenceFinishFailureBlocksPreparation |
| Retain actual failures at handoff boundaries | internal/cli/compact_cadence_replay_test.go | TestCompactCadencePreparationFailurePreservesFinishedInterval |
| Use the same native contract in both launch views | internal/cli/worker_protocol_launch_test.go | TestNativeLaunchRoundtripPreservesBothHostContracts |
| Read exact immutable dispatch without mutation | internal/cli/worker_protocol_launch_test.go | TestNativeDispatchReferenceIsExactAndCreatesNoState |
| Reject unsafe stale or unbound dispatch records | internal/cli/worker_protocol_launch_test.go | TestNativeDispatchReferenceRejectsTamperingAndPolicyDrift |
| Read exact immutable dispatch without mutation | internal/cli/worker_protocol_launch_test.go | TestNativeDispatchRecordUsesSavedPromptAfterTemplateChanges |
| Keep offline verification and quality authority | internal/roles/coordinator_protocol_test.go | TestCoordinatorGuidedProtocol |

## Changed tests

- `internal/contextpack/context_test.go`: strengthen the long-noise fixture so
  its hundred chunks genuinely exceed the response budget; retain tail preview,
  omission and complete original-log recovery assertions (Preserve bounded original-log recovery).
- `internal/cli/compact_replay_test.go`: enlarge the buried-failure fixture to
  exceed the default budget explicitly; retain the middle-only Unicode failure,
  complete noise recovery and charged expansion assertions (Preserve bounded original-log recovery).
- `internal/cli/coordinator_replay_test.go`: allow explicit launch view and
  final-return cadence in the existing lifecycle without dropping native
  dispatch, original packet/skill reads, untrusted findings or final gate oracle (Preserve semantic and terminal handoff decisions).
- `internal/repocheck/size_test.go`: add 200 bounded worker-subsystem lines for
  QE hint generation and lookup scoping; all unrelated caps stay unchanged (Provide bounded executable QE recipes).
- `internal/roles/coordinator_protocol_test.go`: guard conditional finish/prepare
  guidance without dropping existing semantic decision or role authority checks (Preserve semantic and terminal handoff decisions).

## Validation

Three Sol/high subagents implemented and cross-reviewed the changes. Peer
review checked fit-aware results, immutable launch/reference paths, original
packet inputs and semantic/terminal handoff order. An actual-cwd capability
scope issue was corrected with relative-PATH and unknown-cwd regressions. The
full suite exposed a formerly noisy fixture that now fits completely; enlarging
it preserves its original expansion/recovery purpose without weakening fitting
controls. A public JSON field-name mismatch was also corrected.

Required local results:

- `GOWORK=off go build ./...` and `GOWORK=off go vet ./...`: pass.
- `GOWORK=off go test -race -count=1 ./...`: pass, including lifecycle, failure,
  packet/policy freshness, command help/completion and generated-file checks.
  These are Linux runtime tests; no Darwin/Windows runtime result is claimed.
- Pinned `golangci-lint` v2.15.0 for Linux, Darwin and Windows: zero issues.
- Pinned `govulncheck` v1.9.0 for Linux, Darwin and Windows: no vulnerabilities found.
- Newly built `kitsu change check`: every current OpenSpec change passes.
- `GOWORK=off go test ./internal/repocheck -count=1`: pass, covering Markdown
  links, changelog, diagrams, claim boundaries and subsystem size budgets.
- `git diff --check`: pass.

Focused regressions also passed with the race detector, including the exact
fitting-budget cases, genuinely oversized buried failure, both-host original
dispatch retrieval and the complete conditional-cadence lifecycle. Historical
paid exports remain unchanged. Replay calls and serialized UTF-8 bytes are local
proxy measures; provider tokens, costs, model compliance and general quality
equivalence cannot be established from these offline checks. The later paid
pair is descriptive evidence for one task, not general equivalence. Compact remains opt-in.
