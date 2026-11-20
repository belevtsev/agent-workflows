# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
| Resume a configured task from another host | internal/cli/harness_workflow_runner_test.go | TestSupervisedResumeRetainsResultAndRequiresReleaseAuthorization |
| Import accepted worker output | internal/cli/harness_workflow_runner_test.go | TestSupervisedVerificationRejectsWrongResult |
| Verify an accepted candidate | internal/cli/harness_workflow_runner_test.go | TestSupervisedWorkflowDeliversAndKeepsIntakeAssertion |
| Require an independent reviewer | internal/cli/harness_workflow_runner_test.go | TestSupervisedVerificationRejectsWrongResult |
| Resume a disrupted application | internal/cli/harness_workflow_runner_test.go | TestSupervisedResultWaitsForContendedLedger |
| Choose the other host | scenarios/installers/remote_host_test.go | TestRemoteMatchedHostInstall |
| Git reports nothing to push | internal/gate/judge_test.go | TestPushDistinguishesNoUpdatesFromMalformedInput |

## Review coverage

| Findings | Regression |
| --- | --- |
| 1, 3, 5 | Capture exact accepted files. Exercise protected settings, ignored paths, reviewed executable inputs, and the staged candidate. |
| 2, 13 | Require both intake and supplemental results. Change the saved assertion and verify that the prior receipt goes stale. |
| 4, 6, 7 | Reject the old terminal approval and native launch flags. Reject mismatched reviewer profile, model, effort, or signed job. |
| 8, 12 | Call the guard while the ledger is locked. Resume a deferred result from its retained receipt. |
| 9, 10 | Require signed operator release for a stopped job. Check that resume uses the selected attempt after interruption. Native containment acceptance remains open. |
| 11 | Reject a red baseline for the wrong test ID or a check that never executed the expected behavior. |
| 14–16 | Read a larger valid Claude trust file, check exact model families, and run fixture commands from inside Claude Code environments. |
| 17 | Install a matching bundle, preserve the saved host on reinstall, and exercise explicit replacement. |
| 18 | Distinguish empty push input, truncated input and a complete ref update. |

The offline tests use deterministic signed runner fixtures. Native acceptance
with a provisioned profile and signed-in hosts is a separate test matrix.
