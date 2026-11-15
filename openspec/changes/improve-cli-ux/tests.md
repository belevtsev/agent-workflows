# Tests: improve-cli-ux

Tests use local repositories, files, terminal fixtures, and command metadata. Scenario mappings
name implemented tests; the validation notes record observed execution results. No live provider
or model call is needed.

| Scenario | Test file | Text |
| --- | --- | --- |
| Preferred and existing commands share behavior | scenarios/cliux/cliux_test.go | TestCLIUXDefaultsAndSkillInheritance |
| Empty preferred commands show help | internal/cli/ux_help_test.go | TestUXHelpAtEveryDepthIsIndependentOfRuntimeState |
| Metadata reaches every command consumer | internal/cli/ux_reference_test.go | TestUXCommandReferenceIsGenerated |
| Help works before repository and configuration setup | internal/cli/ux_help_test.go | TestUXHelpAtEveryDepthIsIndependentOfRuntimeState |
| Every supported help spelling reaches the leaf | internal/cli/ux_help_test.go | TestUXHelpAtEveryDepthIsIndependentOfRuntimeState |
| Program help flags stay beyond the separator | scenarios/cliux/cliux_test.go | TestCLIUXHelpAndForwardedArguments |
| Invalid leaf arguments give targeted guidance | internal/cli/ux_help_test.go | TestUXUsageErrorsNameTheSpecificHelpAndNeverGuessExecution |
| A typo only suggests a correction | internal/cli/ux_help_test.go | TestUXUsageErrorsNameTheSpecificHelpAndNeverGuessExecution |
| Existing state can be inspected without execution | scenarios/cliux/cliux_test.go | TestCLIUXHelpAndForwardedArguments |
| Small histories do not invent workflow stages | scenarios/cliux/cliux_test.go | TestCLIUXDefaultsAndSkillInheritance |
| Explicit host and preference origins follow precedence | internal/cli/ux_config_test.go | TestUXHostPreferencePrecedenceAndOrigins |
| Local settings remain private to a worktree | internal/cli/ux_config_test.go | TestUXHostPreferenceIsPrivateToEachWorktree |
| Host preference survives gate removal | scenarios/cliux/cliux_test.go | TestCLIUXGateRemovalPreservesHostPreference |
| Noninteractive commands never open an editor | internal/cli/ux_edit_test.go | TestUXEditorsRequireExplicitTerminalAndRejectJSON |
| Editor help and cancellation preserve the draft and files | internal/cli/ux_edit_test.go | TestUXEditorContextHelpPreservesDraftAndUnavailableReasons |
| Previewed saves retain scope and fresh configuration | internal/cli/ux_edit_test.go | TestUXRoleEditorCustomValuesSkillValidationAndConcurrency |
| Nonempty NO_COLOR suppresses editor colors | internal/cli/ux_edit_test.go | TestUXAccessibleEditorRespectsNoColor |
| Editor interrupts return 130 without writes | internal/cli/ux_edit_test.go | TestUXEditorCancellationWritesNothing |
| Adding and removing skills preserve other settings | scenarios/cliux/cliux_test.go | TestCLIUXDefaultsAndSkillInheritance |
| Clear and reset have different inheritance effects | scenarios/cliux/cliux_test.go | TestCLIUXDefaultsAndSkillInheritance |
| Each supported shell receives a completion script | internal/cli/ux_completion_scripts_test.go | TestUXCompletionGeneratedShellSyntaxAndSmoke |
| Callback values remain local and correctly separated | internal/cli/ux_completion_test.go | TestUXCompletionCallbackProtocolAndWordbreaks |
| Path suggestions retain native separators and support nested completion | internal/cli/ux_completion_test.go | TestUXCompletionTasksAdvisersChecksAndPaths |
| Workflow instructions retain existing authority | not automated | Source and guard checks can verify command references and permission boundaries; a live host evaluation of instruction compliance has not been run. |
| Diagram exports match the preferred source labels | internal/repocheck/diagrams_test.go | TestEveryDiagramHasItsSpecItsExportAndAPlace |

`TestUXCanonicalMutationsAndAliasesReceiveGuardProtection` covers preferred mutations, aliases,
shell wrappers, and benign help/inspection paths. `TestCLIUXAdviserActionsPreserveTheTaskRecord`
exercises positional bind/ask/disposition/status/check commands with a local fake adviser,
retaining the saved answer and navigating adviser-only small history without inventing a stage.
Editor coverage additionally checks cancellation, config saves, scope restrictions, changed
inherited configuration, and unavailable skill reasons. Mapping a test names coverage; final
validation below separately records its execution result.
`TestUXCommandReferenceIsGenerated` checks `docs/cli-reference.md` against actual help generated
from metadata, while `TestUXCompletionCommandMetadata` checks the completion consumer. The
reference can be regenerated with
`GOWORK=off go test ./internal/cli -run TestUXCommandReferenceIsGenerated -update-cli-reference`.
`TestUXLegacyNestedHelpKeepsActualArgumentRules` covers explicit legacy hosts, positional versus
flagged task operands, old optional-task inventory, legacy notice defaults, and hidden callback
help. `TestCLIUXAttachesIndependentlyAndInspectsSourceAliases` covers independent attach despite
an older missing source and original realpath/symlink metadata inspection. Adviser regression
coverage also prevents disposition flags from overriding an explicit accept/reject/defer verb.

## Changed tests

- `internal/roles/rules_test.go` and `internal/roles/skills_host_test.go`: update command spelling sentinels to
  preferred `task check` and `skill resolve`, preserving their workflow and authority checks.
- `internal/repocheck/skill_test.go`: use `help --all` for compatibility command inventory and
  accept concise command-table labels, while replaying the existing console transcripts.
- `internal/repocheck/size_test.go`: add a separate 3,500-line CLI presentation budget while
  preserving existing subsystem and prompt limits.
- `internal/repocheck/network_test.go`: permit only the terminal ANSI formatter's `net/url`
  escaping import; network client imports remain restricted to native adviser code.

## Validation

On 15 November 2026, final checks passed on the working checkout after the preference path,
gate removal, interrupt, color, alias help, and generated-reference fixes:

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
GOWORK=off go mod verify
```

The full race run used the temporary PowerShell and Fish directories under
`/tmp/kitsu-completion-shells-4nTPFG` at the front of `PATH`, so all four shell runtime cases ran.
All Go packages, installers, and repository checks passed. Each target's lint reported 0 issues,
vulnerability checks found none, and all modules verified. Native runtime testing ran on Linux;
Darwin and Windows results above are target lint and vulnerability checks.

The built CLI passed `kitsu task check` for all six change records; `git diff --check` passed.
Focused documentation/diagram checks also passed. Existing subsystem and prompt budgets remain
intact, including the 12,000-character coordinator limit and separate 3,500-line CLI budget.

All four changed archify sources passed `finalize` with `showcase`, including validation,
delivery, strict artifact checks, and real Chrome browser checks. Evidence is under
`.archify/cli-ux-20261115/`: `gate.html` and the separate `overview`, `task-workflow`, and
`adviser-opt-in` directories. Each SVG was generated through the actual SVG Auto export action,
retaining both color themes and the embedded font. Export whitespace is normalized in the
existing export harness. All eight light/dark captures were visually inspected; command labels
fit and the existing topology remains readable. Widths are 832, 804, 816, and 828 pixels for
overview, gate, task workflow, and adviser opt-in respectively.

Completion coverage includes readonly local names, malformed configuration, extra arguments,
quoted metacharacters, and option boundaries. All four runtime syntax/smoke cases passed
without final skips: installed Bash/Zsh, temporary Fish 4.2.1 extracted from a package, and
checksum-verified official PowerShell 7.7.0 under `/tmp`. No system shell installation was needed.
`TestUXCompletionZshAutoloadOnFirstQuery` additionally verifies an installed `_kitsu` function
returns suggestions on its first autoloaded query, alongside source-after-compinit setup.
`TestUXCompletionPowerShellPreservesUnicodeAndQuotedValues` passes under forced ASCII console
encoding, preserving Unicode and literal metacharacters without changing the encoding.
`TestUXCompletionFishPreservesQuotedMetacharacters` similarly verifies literal filename values
and no command substitution under Fish 4.2.1.
`TestUXCompletionHomePaths` checks home and nested directory traversal, file/directory filtering,
basename prefixes, and continuation from a suggested native directory separator in an isolated home.

Actual PTY checks exercised F1 help with
wrapped full text and preserved drafts, complete stderr previews, Back retaining choices,
successful Save, and normal Ctrl+C/accessibility Cancel without configuration or lock files.
These were direct terminal smoke checks, with no provider calls or model availability probes.
An additional real-form regression, `TestUXAccessibleEditorRespectsNoColor`, checks nonempty
values `1`, `present`, and `0` with `CLICOLOR_FORCE=1`. Actual normal and accessible PTY checks
confirmed colorless output and cancellation without files after correcting the library default.
`TestUXEditorCancellationWritesNothing` distinguishes explicit Cancel (0) from Ctrl+C/context
interruption (130), with no writes. `TestCLIUXGateRemovalPreservesHostPreference` verifies the
private host preference survives gate removal. The final full checks above include these fixes.

### PR CI follow-up

The initial [PR #230 CI run](https://github.com/belevtsev/kitsu/actions/runs/43751549574)
passed nine checks and failed the native Windows test job. Its only failing test,
`TestUXCompletionTasksAdvisersChecksAndPaths`, expected `/` after directory suggestions;
the implementation correctly returned the native Windows separator `\`.
No review comments or threads were present when the follow-up began.
The corrected test now expects exact native paths and checks completing child files and
directories. Independent review also reproduced incorrect directory listings for `~/` and
`~/nested/`: expanding the whole prefix with `filepath.Join` removed its final separator before
the directory was selected. Splitting the display directory before expanding it fixes this;
five new home-path subtests failed before the production change.
After both fixes, every command in the local validation block above passed again, including
the full race suite with all four completion shells on `PATH`, and lint and vulnerability checks
for Linux, Darwin, and Windows. Darwin and Windows CLI test cross-compilation also passed;
native Windows execution is checked separately by PR CI. Independent re-review found no
additional issue in the home-path fix.

## Limits

No live provider request, host/model execution, workflow quality comparison, human approval,
commit, or publication is claimed. Native Windows PowerShell 5.1 runtime testing remains
unavailable; PowerShell runtime evidence is from version 7.7.0 on Linux. Terminal form fixtures
and actual PTY smoke checks are distinguished from a human usability study.
