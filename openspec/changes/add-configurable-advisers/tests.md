# Tests: add-configurable-advisers

Tests use temporary repositories and local fake executables. No provider account, live model
call, API key, or measured adviser token usage is involved. A mapping names its test location;
the validation notes below state which checks were actually run.

| Scenario | Test file | Text |
| --- | --- | --- |
| No adviser files exist | internal/advisers/config_test.go | TestLoadHasNoAdvisersOrAccessByDefault |
| Repository definitions replace personal definitions | internal/advisers/config_test.go | TestLoadReplacesWholeDefinitionsAndValidatesMergedReferences |
| Invalid adviser configuration is refused | internal/advisers/config_test.go | TestParseStrictConfiguration |
| Role access replaces the default | scenarios/advisers/advisers_test.go | TestCLIConfigurationAccess |
| Inaccessible required adviser is refused | internal/advisers/config_test.go | TestValidateConfigReferencesAndCheckpoints |
| A permitted role compares named choices | scenarios/advisers/advisers_test.go | TestConsultationWorkflow |
| Invalid requests execute nothing | scenarios/advisers/advisers_test.go | TestCLIConfigurationAccess |
| CLI returns an answer | internal/advisers/execute_test.go | TestExecuteUsesLiteralArgvStdinDirectoryAndInheritedAuthentication |
| Failed calls remain unsuccessful | scenarios/advisers/advisers_test.go | TestFailedConsultationsRemainRecorded |
| Shared binding works offline | scenarios/advisers/advisers_test.go | TestCheckpointFreshnessAndOfflineChecks |
| Configuration changes need refresh | scenarios/advisers/advisers_test.go | TestCLIConfigurationAccess |
| Dispositions append to the original answer | internal/advisers/records_test.go | TestRecordsKeepPendingRequestsOriginalAnswersAndEveryDisposition |
| Recorded decision choices match offered choices | scenarios/advisers/advisers_test.go | TestConsultationWorkflow |
| Fresh addressed advice satisfies a checkpoint | internal/advisers/check_test.go | TestCheckpointsDistinguishAttemptsFreshnessAndDisposition |
| Changed content requires fresh advice | scenarios/advisers/advisers_test.go | TestCheckpointFreshnessAndOfflineChecks |
| Future missing advice is not malformed structure | scenarios/advisers/advisers_test.go | TestCheckpointFreshnessAndOfflineChecks |
| Small task history is structurally checked | scenarios/advisers/change_check_test.go | TestSmallTaskRecordChecks |
| Small history moves into a full task | scenarios/advisers/advisers_test.go | TestSmallTaskPromotion |
| Competing histories are refused | internal/advisers/records_test.go | TestRecordPromotionMovesAllHistoryAndRejectsMerge |
| Role asks through the coordinator | not automated | Instructions are reviewed in the source texts; a live host evaluation of model compliance has not been run. |
| Reviewer remains independent | not automated | Reviewer source instructions preserve an independent assessment; a live host evaluation has not been run. |
| Adviser call has no measured usage | internal/cli/adviser_tokens_test.go | TestAdviserTokenReportsKeepMeasuredCountsAndModels |
| Host selection shows model reasoning and access | scenarios/advisers/advisers_test.go | TestCLIConfigurationAccess |
| Roles use their configured routing | scenarios/roles/roles_command_test.go | TestTheDefaults |

Additional focused cases cover literal argv and inherited authentication, answer-file authority,
reader failures, UTF-8 and size bounds, symlink confinement, recursive notices, cancellation,
pending attempts, concurrent record mutations, malformed records, promotion during a pending call,
and matching requester, stage, identity, and content. Role override behavior is also exercised by
`TestARepositoryChangesOneModel` in `scenarios/roles/roles_command_test.go`.

`TestConsultationPreflightRefusesChangedEvidenceBeforeRecordingOrCalling` in
`internal/cli/consult_test.go` covers evidence changes between prompt preparation and execution:
stale evidence produces neither a provider call nor pending history, and fresh evidence runs and
is saved. A separate check after execution also prevents success on changed artifacts.

## Changed tests

- `scenarios/roles/roles_command_test.go`: Differentiated model routing changes the expected
  specialist defaults; the unknown-model case uses the selected host table.
- `scenarios/roles/write_limits_test.go`: Explicit role access and Coordinator brokering keep
  `kitsu-advisers.toml` outside the developer's write limit.
- `internal/cli/guard_test.go`: Adviser command definitions are protected configuration writes.
- `internal/repocheck/structure_test.go`: Direct bounded execution adds `internal/advisers` as an
  allowed caller of the existing subprocess package.
- `internal/repocheck/size_test.go`: The new adviser package has a separate production-code budget;
  existing core and workflow limits reflect the integrations described by the design.

## Validation recorded during documentation work

On 15 November 2026:

- `GOWORK=off go test ./internal/roles -update` regenerated the host files; a subsequent ordinary
  `GOWORK=off go test ./internal/roles -count=1` passed, including unchanged prompt budgets.
- Focused `internal/repocheck` tests for the document index, relative links, heading anchors,
  changelog line lengths, and unmeasured-benefit statements passed.
- `GOWORK=off go run ./cmd/kitsu change check add-configurable-advisers` passed the record's rules.
- `kitsu roles --host codex` and `kitsu consult --help` were checked from the current CLI.
- The overview and task-workflow JSON specs passed archify `finalize` with the `showcase` profile
  and browser checks. Their SVGs came from the viewer's actual SVG Auto export. Light and dark
  previews were inspected at 832 by 544 and 822 by 652 pixels respectively.
- Adviser command flags were checked against Claude Code 2.1.325 and Codex CLI 0.169.0 help.
  No live provider call was made.

The coordinating implementation session also reported these passing checks with `GOWORK=off`:

| Check | Scope | Result |
| --- | --- | --- |
| `go build ./...` | Linux | passed |
| `go vet ./...` | Linux | passed |
| `go test -race -count=1 ./...` | Linux | passed |
| `golangci-lint run ./...` | Linux, Darwin, and Windows targets | passed |
| `govulncheck ./...` | Linux, Darwin, and Windows targets | passed |

After the consultation preflight fix, focused race tests, consultation and failed-call scenarios,
vet, and CLI lint for all three targets passed. Normal and race tests for the final small-history
checks also passed. The coordinating session then reran the final combined tree: build, vet,
the full race-enabled suite, and all three targets' lint and vulnerability checks passed.
Native runtime tests for this change ran on Linux;
targeted lint and vulnerability checks are not native macOS or Windows runtime tests. No result
here substitutes for an independent stage review or human approval.
