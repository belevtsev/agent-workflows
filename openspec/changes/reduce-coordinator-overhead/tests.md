# Verification

Verification uses isolated local Git fixtures, declared fake checks and scripted
fake host/workers. It invokes no model CLI, native paid worker, calibration or
paid reviewer and does not wait for remote CI. Historical frozen evidence stays
unchanged. Byte counts are not provider tokens or quality-equivalence proof.

| Scenario | Test file | Text |
| --- | --- | --- |
| Ordinary assignment delivery | internal/cli/compact_replay_test.go | TestCompactSelectiveReplayRequiredFacts |
| Complete scripted role workflow | internal/cli/coordinator_replay_test.go | TestCoordinatorReplayLifecycle |
| Interrupted Git preparation | internal/cli/coordinator_replay_test.go | TestCoordinatorReplayInterruptedBeforeGit |
| Fresh native dispatch | internal/cli/worker_protocol_test.go | TestNativeDispatchPreservesCompleteContractForBothHosts |
| Valid worker hand-back | internal/cli/worker_protocol_test.go | TestWorkerReturnRetainsUntrustedClaimsWithoutFinishingOrSourceMutation |
| Invalid return input without mutation | internal/cli/worker_protocol_test.go | TestWorkerReturnRejectsInvalidSchemaBeforeMutation |
| Invalid or stale worker hand-back | internal/cli/worker_protocol_test.go | TestWorkerReturnRejectsIdentityPolicyWritesAndStaleChecks |
| Immutable retry and tamper rejection | internal/cli/worker_protocol_test.go | TestWorkerReturnRejectsTamperingAndChangedEvidenceOnRetry |
| Budget preflight | internal/cli/worker_protocol_test.go | TestNativeDispatchSmallBudgetDoesNotWriteSelections |
| Help and completion without setup | internal/cli/worker_protocol_test.go | TestWorkerProtocolHelpAndCompletionAreOffline |
| Managed role boundary | internal/roles/coordinator_protocol_test.go | TestCoordinatorGuidedProtocol |
| Waiting for a native worker | internal/roles/coordinator_protocol_test.go | TestCoordinatorGuidedProtocol |

Both supported hosts retain their original role semantics: Codex stores the body
once in the saved packet, and Claude uses its named native role without a second
body copy. Legacy v1 preparation, task-start opt-in, interval recovery and raw
write/evidence regressions remain covered by the existing suites.

Independent review found no blocking production issue. It requested a stronger
interruption-after-review fixture and removal of redundant introspection from
the canonical v1 replay reference. These are measurement/quality requirements,
not reasons to infer a token benefit. The reference uses the same worker-facing
contract; exact CLI serialization and fake native envelopes are named separately.

Initial local build, vet and full Linux race tests passed. Pinned lint identified
complexity in return validation/ingestion; cohesive validation/retry helpers
replaced the long functions without suppressions. Focused protocol/replay race
tests and all three target lint runs passed after the refactor. Final integrated
local receipts and measured replay results are recorded below.

The independent-review gaps are fixed. Both native dispatch boundaries are
charged in the small saved-handoff replay, and the full v1 reference contains
no redundant role-text/show/skill queries. Both full paths use the same native
worker contract. The interruption fixture records actual current gate evidence
(satisfied in its path-scoped fixture), but still rejects completion without a
Git dispatch/return/finish, task commit and clean worktree.

Finding severities blocker/major/minor are preserved as typed claims. Missing
or invalid severities are rejected rather than dropped into unstructured text.
The full lifecycle retains the major review finding and exact untrusted claims.

Verified replay invocation on 17 November 2026:

`go test -race -count=1 ./internal/cli -run '^Test(CoordinatorReplay|CompactSelectiveReplayRequiredFacts)' -v`

It passed in 6.044 s. New private receipts live under
`~/.local/state/kitsu/coordinator-overhead-20261117/verified-replays/` and are
bound by hashes in the public replay summary. The clean v1 reference recorded
69 calls / 20,659 request bytes / 90,508 response bytes; managed v2 recorded
77 / 25,175 / 121,038. Both executed three checks, reused two and returned six
workers. V2 retained six expansions, one rejected request and one retry.
This is increased transport in one scripted workload, not a model-turn, token,
quality-equivalence or monetary measurement. No old published receipt changed.

All twelve core validation steps passed in
`20261117T070225Z-core-3742744266`: pinned tool versions, build, vet, full Linux
race tests (36.3 s), golangci-lint 2.15.0 for Linux/Darwin/Windows and govulncheck
1.9.0 for the same targets. Generated role/skill and command reference tests,
legacy behavior, protocol bounds and replay cases are included in that suite.
The separate docs batch `20261117T070445Z-docs-3128864130` passed repocheck,
validation-binary construction and all OpenSpec records. A final declared gate
runs after all documentation assembly; no remote CI result is required here.
