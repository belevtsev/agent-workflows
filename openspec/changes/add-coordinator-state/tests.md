# Verification

## Scenario coverage

| Scenario | Test file | Text | What it checks |
| --- | --- | --- | --- |
| Full and small task state | internal/cli/coordinator_state_test.go | TestCoordinatorSnapshotAndDelta | Complete sections, unknown small history, active/cleared interval, legacy output and no completion authority |
| Referenced findings and untrusted returns | internal/cli/coordinator_state_test.go | TestCoordinatorSnapshotRecordedFindingsAndRawChanges | Original recorded path/line and severity, resolved finding removal; policy/return test also separates claims for both hosts |
| Complete output budget | internal/cli/coordinator_state_test.go | TestCoordinatorSnapshotRefusesUnknownBaseAndSmallBudget | A small budget and invalid arguments emit no partial JSON or new baseline |
| Source and ignored record changes | internal/cli/coordinator_state_regression_test.go | TestCoordinatorSnapshotRawEditsHiddenByGitFilter | Raw clean-filter-hidden source changes; coordinator_state_test.go also checks ignored raw task records |
| Policy and evidence changes | internal/cli/coordinator_state_regression_test.go | TestCoordinatorSnapshotExternalSkillsForBothHosts | Selected original skill edits/removal; adjacent regressions cover failed/expired/damaged evidence and offline adviser checkpoints |
| Unchanged and changed delta chains | internal/cli/coordinator_state_test.go | TestCoordinatorSnapshotAndDelta | Empty unchanged sections, check-only changes, interval clearing and exact full-projection reconstruction |
| Invalid or unavailable baseline | internal/cli/coordinator_state_test.go | TestCoordinatorSnapshotRejectsInvalidBaselines | Unknown id, task/worktree/version mismatch, expired/future/incomplete/null bases; other regressions cover tampering and unsafe links |
| Safe retention and observation | internal/cli/coordinator_state_test.go | TestCoordinatorSnapshotPrivateCacheSafetyAndRetention | Private cache files, entry-count bound, active interval preserved and tracked history unchanged |

`internal/cli/coordinator_state_regression_test.go` additionally covers a busy
ledger, linked canonical records and deterministic comparison of observations
across a source change. Existing managed-interval tests retain atomic publication
and pinned-directory safety coverage. CLI metadata/help/completion remain checked
by the existing UX suite. The state component has its own bounded production-code
budget; existing component budgets are unchanged.

## Evidence limits

These are local scripted tests, not provider/model evaluations. They exercise
the real CLI in isolated Git fixtures, a local check child and original skill
files. The normalization child is a Go test helper, not a native worker. Views
execute no model or adviser; the offline adviser fixture uses an unavailable
command and requires no consultation record. Passing assertions do not establish
semantic TDD, independent native review or real-task quality equivalence.

The unchanged-delta assertion checks fewer serialized response bytes than the
full snapshot. It does not estimate provider tokens or total workflow savings.
No new paid pilot, provider calibration or CI wait is part of this change.

## Local checks

- The initial snapshot regression failed on the missing `--compact` option before implementation.
- Build, vet and the full Linux race suite passed after the final production refactor; the race suite took 36.03 seconds.
- Pinned golangci-lint 2.15.0 and govulncheck 1.9.0 passed for Linux, Darwin and Windows targets. Native macOS/Windows execution is not claimed.
- Repository link/changelog/code-size checks and generated CLI help/reference checks passed. OpenSpec's exact task/requirement and scenario/test mappings pass `kitsu change check add-coordinator-state`.
- An actual local CLI snapshot of this change returned all eight sections in 3292 response bytes. With no intervening change, its delta returned `sections:{}` in 577 bytes, 82.5% fewer response bytes. The view still reported incomplete record prerequisites and no completion authority.

The final core receipts are retained privately under batch
`20261117T073204Z-core-1943799851`; the earlier version checks confirmed the
tool pins. The byte sample is one unchanged observation in this checkout,
including its envelope and newline. It is not a completed-workflow comparison,
provider-token measurement or quality-equivalence result. No CI wait or paid run
was performed.
