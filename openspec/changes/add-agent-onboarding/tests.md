# Tests: add-agent-onboarding

Scenario mappings name the implementation's tests; validation evidence is recorded separately.
No unexecuted check is recorded as passed. Local fixtures can verify command behavior and
configuration interpretation; they cannot prove live host skill loading or hook trust.

| Scenario | Test file | Text |
| --- | --- | --- |
| Supported local setup is inspectable | internal/onboarding/inspect_test.go | TestInspectConfiguredHosts |
| Missing or disabled components give next steps | internal/onboarding/inspect_test.go | TestInspectPluginProblems |
| Diagnostics run without execution or writes | internal/onboarding/inspect_test.go | TestInspectReadOnlyAndSanitized |
| Configured hooks retain unknown runtime evidence | internal/onboarding/inspect_test.go | TestInspectConfiguredHosts |
| Unsupported local evidence stays unknown | internal/onboarding/inspect_test.go | TestInspectPluginProblems |
| Onboarding help works before setup | internal/cli/ux_help_test.go | TestUXHelpAtEveryDepthIsIndependentOfRuntimeState |
| Onboarding follows the effective host preference | internal/cli/onboarding_test.go | TestOnboardingHostChoiceUsesPreferences |
| Handoff-only modes remain usable with broken setup | internal/cli/onboarding_test.go | TestOnboardingReadOnlyModesNeverStartHostOrCreateTask |
| Requests remain literal task text | internal/cli/onboarding_test.go | TestOnboardingParsesOneLiteralRequest |
| Ready local preflight launches the selected host | internal/cli/onboarding_terminal_linux_test.go | TestOnboardingInteractiveStartPreservesChildExitTwo |
| Missing terminal or broken preflight prevents launch | internal/cli/onboarding_test.go | TestOnboardingReadySetupRequiresInteractiveTerminal |
| An unconfigured repository gets workflow guidance | scenarios/plugin/context_test.go | TestSessionContextWithoutADeclaration |
| Configured context preserves accurate gate facts | scenarios/plugin/context_test.go | TestSessionContextWhenHooksAreNotInstalled |
| Missing CLI gives guidance before repository setup | scenarios/plugin/instructions_test.go | TestSessionContextWhenKitsuIsNotInstalled |
| Bootstrap never executes a repository binary | scenarios/plugin/commands_test.go | TestHookCommandsRunNoKitsuOfTheRepository |
| Onboarding docs distinguish setup from execution | not automated | Link and reference checks validate structure; truth of the setup/runtime explanation needs source review and no live host execution is claimed. |

`TestOnboardingHandoffKeepsTheHostAsOrchestrator` checks each host's task-skill prompt.
`TestOnboardingReadOnlyModesNeverStartHostOrCreateTask` also covers blocked default launch,
uncontaminated JSON, help/completion without execution, and absence of task or config writes.
`TestOnboardingLaunchHonorsCancellation` covers cancellation of the fake host process.
`TestContextTextStaysWithinItsLimit` checks the combined workflow introduction and each gate
state across both hosts and host-neutral output; `TestSessionContextInAGatedRepository` also
covers the installed gate. Existing hook tests retain no-copy, no-version, safe relative PATH,
and no-absolute-PATH behavior. These rows identify coverage, not execution results.

`TestInspectHookProblems` and `TestInspectTaskSkillProblems` also cover disabled, malformed,
missing, and non-invocable required components. `TestInspectSettingsPrecedence` covers host
configuration layers and managed restrictions; `TestInspectAbsolutePATH` covers safe lookup.
`TestInstalledHookBundles` inspects both actual repository hook bundle definitions without
executing them. `TestInspectOptionalGitGate` checks the separate nonblocking gate finding.
`TestUXCommandReferenceIsGenerated` checks the generated reference against command metadata.
`TestInspectCodexPluginFeature` covers the effective plugins feature across system, user,
project, and managed requirements layers, including the independently reproduced false-ready
case. `TestInspectSettingsPrecedence` also records that Claude skillOverrides does not apply to
plugin skills, as the host's official skill guide specifies.

`internal/skills/metadata_test.go` checks ReadMetadata with
`TestReadMetadataKeepsInvocationRulesWithoutApplyingHostSettings` and
`TestReadMetadataRejectsInvalidSourceAndInvocationMetadata`. This separates original invocation
metadata from effective host policy, which onboarding applies for the selected installation.
The Windows-only `TestWindowsSessionStartMissingCLIProvidesOnboarding` in
`scenarios/plugin/onboarding_windows_test.go` covers the native fallback before gate setup;
compilation is recorded separately from native runtime execution.

`scenarios/onboarding/onboarding_test.go` uses real installed-bundle fixtures with fake host
executables. `TestInstalledBundleCheckAndHandoffStayReadOnly` and
`TestMissingHostStillProducesAnActionableHandoff` cover both hosts and the missing-host path.
The launch tests use only fake child processes. Shared foreground-process tests cover literal
argv, streams, environment isolation, status propagation, start failure, cancellation, and Unix
foreground process-group preservation. No test name implies a real host workflow was executed.

## Validation

On 16 November 2026, generated command documentation was updated successfully with:

```sh
GOWORK=off go test ./internal/cli -run TestUXCommandReferenceIsGenerated -update-cli-reference
GOWORK=off go test ./internal/repocheck -run 'Test(RelativeMarkdownLinksResolve|MarkdownAnchorsResolve|ChangelogLineLength|TheIndexNamesEveryDocument)$'
GOWORK=off go run ./cmd/kitsu task check add-agent-onboarding
```

The focused documentation checks and the in-progress task record check passed. After the
Codex plugin-feature fix, the final repository checks below also passed on 16 November 2026:

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
git diff --check
```

The full race run put /tmp/kitsu-completion-shells-4nTPFG/powershell and its fish/usr/bin
directory on PATH alongside the installed shells, so all four completion shell runtime cases
ran. Every package passed, including onboarding, foreground process, plugin, scenario, installer,
and repository checks. Each target's lint reported zero issues, vulnerability checks found none,
and all modules verified. Native runtime execution was on Linux.

Test binaries cross-compiled with CGO_ENABLED=0 and go test -c for both GOOS=windows and
GOOS=darwin: internal/cli, internal/onboarding, internal/proc, scenarios/plugin, and
scenarios/onboarding. The Windows plugin binary includes the new missing-CLI bootstrap test.
These are compiler checks; no native Darwin or Windows runtime was exercised this turn.

Existing production-code budgets passed: gate 6,108/6,250 lines, roles 3,795/4,000, advisers
3,604/4,000, skills 1,808/2,000, and CLI presentation 2,978/3,500. The separate onboarding budget
is 1,118/1,200. Generated prompt budgets remain intact; complete workflow-plus-gate context
stays within 1,000 characters. No diagram source or topology changed.

The generated command-reference golden test and `claude plugin validate .` also passed.
Claude validates its documented manifest subset; this supplies no safety/policy review,
trusted-hook execution, or model result.

Readonly smoke checks used /tmp/kitsu-agent-onboarding/kitsu with that directory on PATH in
the real local environment. `agent check --host codex --json` returned exit 5 and ready:false:
Kitsu and host CLI lookup passed, plugin/task/hooks failed because Kitsu was not installed and
enabled locally, and hook_runtime warned. The optional git_gate check passed its file/setup
inspection without checking declared evidence. `task start 'Explain the Kitsu workflow'
--host claude --json` returned exit 0 and ready:false with the prompt
`/kitsu:task Explain the Kitsu workflow` and two proposed argv elements. It opened no host session.

Local help was also checked without a session or install: Codex CLI 0.169.0 accepted
`codex plugin --help`, `codex plugin marketplace add --help`, and `codex plugin add --help`;
Claude Code 2.1.325 accepted `claude plugin install --help`. These confirm the documented command
grammar only; no plugin was installed and no model was invoked.

Targeted review rebuilt an isolated fixture binary under /tmp and reran the Codex disabled-plugin
case after its fix: agent check returned exit 5 and ready:false. The Claude plugin fixture with
standalone skillOverrides remained exit 0 and ready:true, matching the documented plugin exemption.
This is local diagnostic evidence, not a live host workflow; `reviews.md` records the review scope.

### PR CI follow-up

The initial [PR #231 CI run](https://github.com/belevtsev/kitsu/actions/runs/43820935540)
passed nine checks and failed the native Windows test job. Two test expectations failed:
`TestForegroundCancellationAndStartFailure` assumed a killed child always reports Unix's
exit status `-1`, but Windows reports `1` with `Stopped` and `Cancelled` set;
`TestCodexWindowsSessionStartSaysWhenKitsuIsMissing` still expected silence outside Git,
where the new onboarding contract requires one-line install/PATH guidance.
No PR review comments or inline review threads were present when the follow-up began.
The foreground regression now checks the exact native killed-child status separately from
`Stopped` and `Cancelled`, and verifies that an ordinary child exit `1` is not cancellation.
Pre-start cancellation still requires no child output and the cancellation error. Windows
bootstrap assertions now require exactly the one-line install/PATH message and exit `0`
across gated, undeclared, nested, and outside-Git locations, with and without Git on PATH and
both `cmd.exe` invocation forms. Planted-binary lookup protections remain checked. Production
behavior is unchanged; the process-result API comment now describes the native status correctly.
After these corrections, local build, vet, the full race suite, all three targets' lint and
vulnerability checks, module verification, and diff checks passed again. Focused foreground
cancellation tests also passed 100 repetitions. The corrected process and Windows platform
test binaries cross-compiled successfully; native Windows execution is verified separately
by the follow-up PR CI run.

## Limits

No live model, authentication, trusted-hook execution, workflow quality comparison, formal
stage review, human approval, commit, or release publication is claimed by this record.
