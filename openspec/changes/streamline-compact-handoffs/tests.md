# Verification

Ordinary tests use isolated local Git repositories, file input and fake check
executables. They invoke no model, native worker launch or paid pilot.

| Scenario | Test file | Text |
| --- | --- | --- |
| Native read and search | internal/cli/compact_typed_test.go | TestTypedContextReadsAndSearchesNativeOperands |
| Invalid typed request | internal/cli/compact_typed_test.go | TestTypedContextRejectsInvalidFlagsBeforeOpeningCache |
| Direct saved handoff | internal/cli/worker_dispatch_test.go | TestSavedWorkerBriefDeliversOriginalCompleteAssignmentOnce |
| Invalid assignment | internal/cli/worker_dispatch_test.go | TestSavedWorkerBriefInputErrorsDoNotWriteSelection |
| Prepare and finalize a worker | internal/cli/compact_stage_test.go | TestStagePrepareFinishOrdersReceiptsAndBlocksOverlap |
| Invalid writes or changed policy | internal/cli/compact_stage_test.go | TestStageWriteViolationLeavesStageOpenForRepair |
| Expected check outcome | internal/cli/compact_stage_test.go | TestStageChecksExpectationsUseExistingCurrentEvidence |
| Explicit abandonment | internal/cli/compact_stage_test.go | TestStageStalePolicyCanOnlyBeAbandoned |
| Ordinary task and help | internal/cli/compact_test.go | TestTaskStartCompactModeIsExplicitAndHandoffOnly |

Additional regressions cover literal shell characters and dash operands,
cache equivalence, task metadata, containment, omitted results, cursor recovery,
Claude native role references, assignment fidelity, active-interval blocking,
check-policy changes, idempotent finalization, worktree isolation and stale evidence.
Independent review additionally exercises interrupted saved-file publication,
normalized hidden writes, forged completion receipts and safe crash recovery.

On 17 November 2026 the following passed:

- `GOWORK=off go build ./...` and `GOWORK=off go vet ./...`.
- `GOWORK=off go test -race -count=1 ./...` on Linux, including typed CLI,
  canonical brief, raw-write, forged receipt and contained ledger regressions.
- Pinned `golangci-lint` 2.15.0 and `govulncheck` 1.9.0 for Linux, Darwin and Windows.
- Generated command-reference validation and whole-repository repocheck.
- Both updated diagrams passed archify showcase validation and browser checks;
  actual SVG Auto exports were inspected in light and dark themes.

Core receipts: `~/.local/state/kitsu/compact-handoffs-20261117/receipts/20261117T044658Z-core-4154987830/`.
All twelve tool/version/build/test/scan steps exited zero. Validation uses
disk-backed temporary storage; no old pilot directory or frozen input was changed.
Diagram receipts: `.archify/context-engine/typed-context-review/` and
`.archify/context-engine/typed-overview-review/`. Code-size checks retain the
existing engine budget and bound worker dispatch/interval code separately.

Repocheck and the separate validation executable passed in docs batch
`20261117T045316Z-docs-3413282328`. Record verification initially requested a
missing `## Rules` table in the design. After adding the invariant table,
`kitsu change check` passed in `20261117T045354Z-docs-2532371814`.
Both batches are retained beside the core receipts. Final declared checks run
with that separate executable after these receipt edits; they launch no host.
Native macOS/Windows runtime and live host dispatch remain unverified here.
No new claim about token saving or general quality equivalence is made.
