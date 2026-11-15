# Tests: add-existing-skills

Tests use temporary local files, settings, plugins, repositories, and receipts. No live host or
model process is needed. Mappings identify implemented tests; the validation notes below state
which commands were actually run.

| Scenario | Test file | Text |
| --- | --- | --- |
| Existing sources are listed without execution | internal/skills/discovery_test.go | TestSkillRepositoryAndPersonalDiscovery |
| Explicit roots expose existing local sources | internal/skills/catalog_test.go | TestSkillExplicitPathsPreserveOriginalDirectory |
| Metadata retains YAML values and diagnostics | internal/skills/catalog_test.go | TestSkillYAMLAndFingerprints |
| Duplicate names need qualified references | internal/skills/discovery_test.go | TestSkillRepositoryAndPersonalDiscovery |
| Invalid references do not change bindings | internal/cli/skills_test.go | TestSkillCLIErrorsLeaveRoleFilesUnchanged |
| Disabled sources cannot be attached | internal/skills/discovery_test.go | TestSkillHostDisabledPathsCannotBypassPolicy |
| Manual-only and background metadata stay distinct | internal/skills/catalog_test.go | TestSkillInvocationSemantics |
| Repository skill arrays replace personal defaults | internal/roles/skills_config_test.go | TestLoadSkillPrecedence |
| Empty configuration pins no skills | internal/roles/skills_config_test.go | TestLoadSkillPrecedence |
| Personal and coordinator scopes stay limited | internal/roles/skills_config_test.go | TestParseSkillSelectionsStrict |
| Set clear and omission have distinct effects | internal/cli/skills_test.go | TestSkillCLISelectionsPreserveModelsAndScope |
| Reset applies to the chosen scope | internal/cli/skills_test.go | TestSkillCLISelectionsPreserveModelsAndScope |
| Role output exposes current skill selections | internal/cli/skills_test.go | TestSkillCLIInventoryAndRoleDisplays |
| Resolution returns current original sources | internal/skills/catalog_test.go | TestSkillYAMLAndFingerprints |
| An unavailable selection stops the handoff | internal/cli/skills_test.go | TestSkillCLIInventoryAndRoleDisplays |
| A task selection records portable source identities | internal/cli/skills_test.go | TestSkillCLIResolveIsReadOnlyAndRecordsSelections |
| Changed selections preserve earlier receipts | internal/skills/selection_test.go | TestSkillSelectionsArePortableImmutableAndOffline |
| Small skill history promotes without merging | internal/skills/selection_test.go | TestSkillHistoryPromotesWithoutLosingReceipts |
| Shared selections can be checked offline | scenarios/skills/skills_test.go | TestSkillCLIRecordsChecksAndPromotesSelections |
| Unsafe or malformed histories fail | internal/skills/selection_test.go | TestSkillReceiptsRejectUnsafeLocationsAndInvalidSelections |
| Worker receives original skill instructions | internal/roles/skills_host_test.go | TestSkillsReachWorkersWithoutChangingRoleBoundaries |
| Skills preserve workflow authority | not automated | The role texts preserve write limits, review independence, finding rules, and human approvals; instruction compliance in a live host has not been evaluated. |

Additional discovery cases cover installed Claude plugins, scoped installation ambiguity,
configured Codex plugin caches, unknown cache versions, malformed plugin metadata and escaping
manifest paths in `internal/skills/plugins_test.go`. `TestSkillMalformedEntriesAreDiagnostic`
covers invalid YAML, duplicate keys, missing fields, wrong types, and bounded files;
`TestSkillYAMLAndFingerprints` also leaves script and command-substitution sentinels unexecuted.
`TestSkillSymlinkedCollectionsAndCycles`, `TestSkillClaudeVisibilitySettings`, and
`TestSkillMalformedSettingsFailSelectionWithoutLeakingContents` cover source aliases, host
visibility, and safe settings diagnostics. CLI error tests cover missing, ambiguous, disabled,
manual-only, duplicate, and invalid-scope selections without partial writes.
`TestSkillCodexNameRulesAndBundledPolicy` covers ordered name/path selectors, repository
precedence, and bundled `.system` policy; `TestSkillCodexInvocationSidecarPolicy` covers
`agents/openai.yaml` invocation policy, default availability, and invalid sidecar diagnostics.

`TestEditSkillSelectionPreservesFields` covers ordinary, dotted, and inline TOML while keeping
model fields, comments, and paths. `TestGlobalRoleSkillWritesPreserveModeAndRejectLinks` in
`internal/cli/roles_edit_test.go` covers concurrent personal edits, original file permissions,
and final-path symlink refusal. `TestConcurrentSkillReceiptsKeepEverySelection` preserves every
concurrent task selection. Read-only CLI resolution checks no task folder is created, and
explicit-root selections remain resolvable without repeating the root.

`TestSkillCLIRecordsChecksAndPromotesSelections` also checks global discovery of small history,
offline named checks after removing the source, unchanged history after failed preflight,
full-history promotion, and CLI tamper rejection. `TestSkillsReachWorkersWithoutChangingRoleBoundaries`
checks all generated Claude roles have `Skill`, cannot start workers, and keep read-only roles'
write-tool limits; it checks the coordinator's source handoff and truthful loading guidance.
These are file and instruction checks, not a live demonstration of host loading or compliance.

Independent receipt review added `TestSkillSelectionTemporaryEntriesRejectUnsafeTypes` in
`internal/skills/selection_test.go`, plus `TestSkillChecksRejectLinkedFullTaskBeforeReadingTarget`
and `TestSkillChecksRejectUnsafeTemporaryEntries` in `scenarios/skills/skills_test.go`. They
cover unsafe temporary-entry types and full task symlinks, including rejecting a linked task
before reading its target. These regressions failed against the isolated pre-fix source and
passed under race checks after the fixes. The review covered receipt paths and offline history
handling; it supplies no complete-stage review or human approval.

## Changed tests

- `internal/cli/roles_edit_test.go`: add personal skill binding persistence coverage for Explicit
  selection editing; existing model-edit concurrency assertions remain intact.
- `internal/repocheck/size_test.go`: add the separate 2,000-line skill implementation budget from
  the design while preserving existing core, workflow, and adviser budgets.
- `internal/kitsutest/kitsutest_test.go`: add `TestRepositoryIsolatesInheritedAppData` so personal
  role-binding fixtures stay isolated from inherited Windows application configuration.

## Validation

On 15 November 2026, the coordinator validated a skills-only source snapshot in
`/tmp/kitsu-skills-verify-20261115`, based on `main` at `68a9d49`. Concurrent unrelated adviser
changes in the shared working tree were excluded. These checks passed on that isolated snapshot
after the receipt-review follow-up fixes:

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

Lint reported 0 issues and vulnerability checks found no vulnerabilities on each target. The
full race suite includes documentation links and anchors, the changelog, generated host files,
prompt and production-code budgets, all skill/role/CLI packages, and real CLI scenarios. Native
runtime tests ran on Linux; Darwin and Windows results are target lint and vulnerability checks.

The CLI built from that snapshot passed `kitsu change check add-existing-skills` and global
`kitsu change check` for all four task records. `git diff --check --cached` passed. After
receipt-review fixes, this focused suite passed:

```sh
GOWORK=off go test -race -count=1 ./internal/skills ./scenarios/skills ./internal/kitsutest
```

Focused lint for those three packages reported 0 issues; the full checks above then passed
again. Independent re-review confirmed both receipt findings resolved with no further material
issues in that targeted scope. Read-only inventory smoke checks also succeeded for the current
user's Codex and Claude catalogs. No complete-stage review, human approval, or live skill
loading has been recorded.

### Integration with current main

On 15 November 2026, the skills changes were integrated with `main` at `3e343ce`, which includes
the optional native advisers from PR #228. The 41 files outside overlapping changes matched the
validated skills snapshot byte for byte; the 18 shared files retain both features. The coordinator
wording was shortened to 11,977 characters to fit its existing 12,000-character budget, and the
host files were regenerated.

Build, vet, the complete race suite, and lint and vulnerability checks for Linux, Darwin, and
Windows targets all passed on the integrated source. Read-only CLI smoke checks covered both
host inventories, role output, coordinator resolution, help, and the offline adviser catalog.
No skills were pinned by default; advisers remained experimental and disabled by default.

## Limits

No live Codex or Claude Code worker-loading evaluation, quality benchmark, or token comparison
was performed. Local discovery cannot prove the host's complete effective catalog, plugin trust,
successful loading, or compliance with loaded instructions.
