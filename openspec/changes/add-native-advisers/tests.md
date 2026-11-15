# Tests: add-native-advisers

All provider fixtures are local HTTPS servers. No paid provider or live model is contacted.

| Scenario | Test file | Text |
| --- | --- | --- |
| Credentials and discovery enable no advisers | internal/advisers/config_test.go | TestLoadHasNoAdvisersOrAccessByDefault |
| Definitions require explicit role access | internal/cli/adviser_native_test.go | TestNativeConsultationRequiresExplicitAccess |
| Explicit native definitions coexist with legacy CLIs | internal/advisers/native_validation_test.go | TestNativeConfigurationAndLegacyCompatibility |
| Typed results preserve uncertainty | internal/advisers/native_test.go | TestNativeProvidersAndRequestMapping |
| Invalid preparation makes no attempt | internal/cli/adviser_native_test.go | TestNativePreflightRejectsBeforeRecording |
| HTTP failures do not retry or reveal credentials | internal/advisers/native_test.go | TestNativeFailuresAreBoundedAndDoNotRetry |
| Redirects and stopped calls remain bounded | internal/advisers/native_test.go | TestNativeRefusesRedirects |
| Typed history is immutable and readable offline | internal/advisers/native_validation_test.go | TestNativeRecordValidationAndOfflineReads |
| A native receipt needs disposition and fresh content | internal/cli/adviser_native_test.go | TestNativeConsultationWorkflowAndOfflineHistory |
| Failed and stale calls retain answers without satisfying checkpoints | internal/cli/adviser_native_test.go | TestNativeFailedAndStaleCallsKeepHistory |
| Mixed CLI and API usage is partial | internal/cli/adviser_native_test.go | TestNativeUsageMixedWithCLIStaysSeparateFromHost |
| Provider discovery needs no repository or credentials | internal/cli/adviser_native_test.go | TestAdviserProviderCatalogIsOfflineAndNeedsNoRepository |
| Host instructions explain native judgments | internal/roles/hostfiles_test.go | TestHostFilesAreMadeFromTheTexts |

`TestNativeCancellationAndTimeout` additionally checks stopped calls while reading a response.
Instruction semantics have not been evaluated with live models.

## Changed tests

- `TestLoadHasNoAdvisersOrAccessByDefault` also exercises present provider credentials and verifies no configuration is created.
- `TestAdviserProviderCatalogIsOfflineAndNeedsNoRepository` checks the experimental and disabled-by-default catalog metadata.

## Observed validation

Validated on 15 November 2026 in an isolated native-adviser worktree based on `main`, on Linux:

- `GOWORK=off go build ./...` — passed.
- `GOWORK=off go vet ./...` — passed.
- `GOWORK=off go test -race -count=1 ./...` — passed all packages and scenarios.
- `GOWORK=off golangci-lint run ./...` — passed with zero issues for Linux and with
  `GOOS=darwin` and `GOOS=windows`.
- `GOWORK=off govulncheck ./...` — no vulnerabilities found for Linux and with
  `GOOS=darwin` and `GOOS=windows`.
- `GOWORK=off go run ./cmd/kitsu change check` — all change folders obeyed their rules.
- `git diff --check` — passed. Provider catalog and consult-help commands were inspected locally.

The optional/experimental update also passed the full race suite, cross-target lint and vulnerability
checks. Its regressions verify credential-only defaults, denied consultation without access, a
successful explicit opt-in, and provider catalog status. Updated host texts fit their existing budgets.

The full race suite includes local native HTTPS tests, legacy CLI adviser scenarios, generated
host-file comparisons, prompt budgets, documentation links, the adviser code budget, and network
dependency boundaries on all three target platforms. Runtime execution was on Linux; cross-target
lint, dependency inspection, and vulnerability checks do not claim native macOS/Windows runtime tests.

## Limits

No live API integration, model quality, throughput, or comparative token-saving evaluation was
performed. Native identity and usage are provider-reported. The separate existing-skills
implementation is not included in this worktree or its validation.
