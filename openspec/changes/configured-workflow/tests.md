# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
| Install a matched host and save its default | scenarios/installers/remote_host_test.go | TestRemoteMatchedHostInstall |
| Initialize role assignments from the saved default | internal/cli/roles_workflow_test.go | TestInitializeRolesFromInstalledHost |
| Allow validated role settings from either plugin | internal/cli/guard_test.go | TestGuardAllowsDirectConfiguredRoleSettings |
| Read the completed native handback after delayed session registration | internal/onboarding/workflow_test.go | TestWorkflowCompletesWithDelayedSessionRegistration |
| Resume with the pinned role assignment | internal/cli/worker_managed_test.go | TestConfiguredManagedWorkerUsesPinnedRoute |
| Verify the exact candidate without changing the assertions | scenarios/harness/verification_candidate_test.go | TestVerificationRejectsIntermediateRawChanges |
| A worker proposes an unapproved assertion | scenarios/harness/configured_workflow_test.go | TestConfiguredWorkflowDoesNotExecuteUnapprovedWorkerArgv |
| Managed design waits for specification approval | internal/cli/worker_managed_test.go | TestConfiguredManagedFullStageRequiresOpenSpecApprovals |
| Reuse a completed managed worker handback | internal/cli/worker_managed_test.go | TestConfiguredManagedWorkerUsesPinnedRoute |
| A configured review parent returns without a native reviewer | internal/cli/worker_managed_test.go | TestConfiguredManagedReviewRequiresNativeHooks |
| Refuse a managed commit that omits a changed path | internal/cli/worker_managed_test.go | TestConfiguredManagedGitUsesExplicitStagedPaths |
