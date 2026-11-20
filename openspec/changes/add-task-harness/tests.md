# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
| Resume configured story intake after a result or interruption | internal/cli/adviser_story_review_test.go | TestConfiguredStoryReviewIntake |
| Resume saved verification in either host | scenarios/harness/feature_handoffs_test.go | TestFeatureHandoffReplaysInBothHosts |
| Deliver JSON export | scenarios/harness/feature_delivery_test.go | TestVerifiedFeatureDelivery |
| Revise scope or withdraw check approval | scenarios/harness/feature_delivery_test.go | TestVerifiedFeatureDelivery |
| Review an incomplete story | scenarios/harness/feature_delivery_test.go | TestFeatureDraftAndNamedCheck |
| Continue an approved design | scenarios/harness/harness_test.go | TestJavaScriptPlanThenImplement |
| Review a named committed range | scenarios/harness/committed_review_test.go | TestCommittedReviewRange |
| Require independent review by request or repository policy | scenarios/harness/review_policy_test.go | TestImplementationReviewPolicy |
| A coding ticket survives edits on its original base | scenarios/harness/work_ticket_test.go | TestWorkTicketSurvivesEditsButNotANewBase |
| Review an existing candidate | scenarios/harness/harness_test.go | TestCppReviewOnlyPreservesIndexAndWorkingTree |
| Repair and recover | scenarios/harness/harness_test.go | TestGoExistingFixRepairVerificationAndRecovery |
| Keep the opening check policy through delivery | scenarios/harness/verified_policy_test.go | TestChangedDeclarationBlocksVerificationAndCommit |
| Require permission to change a failed verify-only candidate | scenarios/harness/verified_policy_test.go | TestChangedVerifyOnlyCandidateNeedsRepairAuthority |
| Approve declared checks from the developer's terminal | internal/cli/ux_check_trust_test.go | TestReviewExactCheckDeclarationBeforeAgentExecution |
| Confirm publication | scenarios/harness/harness_test.go | TestPublishChecksActualBranchBeforeCompleting |
| Identify when the verified commit already existed | scenarios/harness/commit_history_test.go | TestDeliveryDistinguishesTheCommitAlreadyPresentAtVerification |
| A staging-only change reopens review | scenarios/harness/harness_test.go | TestStagedReviewChangesWithIndexOnly |
| Interrupted verification resumes | scenarios/harness/harness_test.go | TestInterruptedProofNeedsFinalObservation |
| Follow a brief action receipt | scenarios/harness/harness_test.go | TestBriefRetainsDecisionAndExpandsFullFindings |
| Distinguish passing verification from commit readiness | scenarios/harness/review_policy_test.go | TestVerifyFirstReportsCheckedTreeUntilReviewClearsDelivery |
| Stage the verified tree for commit | scenarios/harness/harness_test.go | TestVerifiedCommitTicketSurvivesStagingExactlyTheVerifiedTree |
| Recover a delegated implementation or repair | scenarios/harness/delegated_work_test.go | TestDelegatedWorkRecovery |
| Recover a delegated implementation or repair | scenarios/harness/delegated_work_test.go | TestLinkedDeveloperCanReturnAfterDeadline |
| Resolve reviewer settings at reservation | scenarios/harness/review_settings_test.go | TestReviewReservationResolvesHostSettingsOnce |
| Verify a required review came from a native worker | scenarios/harness/native_review_test.go | TestRequiredReviewNeedsNativeWorkerOnCurrentCandidate |
| Extend a partial review to delivery | scenarios/harness/harness_test.go | TestReviewScopeExtensionChecksTheWholeDeliveryCandidate |
| Pin limits before launching a host | internal/cli/onboarding_terminal_linux_test.go | TestInteractiveHarnessPinsScopeAndResumesOriginalTask |
| Bound independent review starts | scenarios/harness/review_limits_test.go | TestReviewDispatchLimitPreservesFindingsAndCanResume |
| Accept a completed reserved review | scenarios/harness/review_limits_test.go | TestReviewFinishingAfterDeadlineCanReturnCurrentResult |
| Reserve review with a result | scenarios/harness/review_limits_test.go | TestRecordAndClaimReviewSurvivesRetries |
| A commit has an unexpected merge parent | scenarios/harness/commit_history_test.go | TestUnreviewedMergeParentCannotCompleteDelivery |
| Finish a pending merge | scenarios/harness/commit_history_test.go | TestPendingMergeMustActuallyBeCommitted |
| A hook rewrites the verified tree | scenarios/harness/commit_history_test.go | TestCommitHookTreeRequiresFreshEvidenceAndKeepsTheCommit |
| A hook rewrites the verified tree | scenarios/harness/commit_history_test.go | TestCommitHookPreservesStagingChangedDuringCommit |
| Merge parents change during verification | scenarios/harness/commit_history_test.go | TestChangingMergeParentsDuringVerificationRequiresAnotherAssertion |
| Brief verification names the executed check | scenarios/harness/verification_output_test.go | TestBriefVerificationPointsToExecutedLog |
| Stream full verification output | scenarios/harness/verification_output_test.go | TestFullVerificationStillStreamsDiagnostics |
| Inspect the original staged and working candidates | scenarios/harness/review_diff_test.go | TestReviewerCanInspectPinnedStagedAndWorkingCopies |
| Inspect a daily task without a worker receipt | scenarios/tokens/daily_session_test.go | TestDailyTaskShowsWholeSessionEvidenceWithoutChargingItsTotal |
| Preserve the opening inputs of an unfinished read-only plan | scenarios/harness/stage_outcomes_test.go | TestReadOnlyPlanKeepsOpeningCandidateAcrossResume |
| Resume after an accounting read error | scenarios/harness/review_settings_test.go | TestBriefReviewCanProceedWithoutReadableUsageHistory |
| Read a reserved reviewer input | scenarios/harness/reviewer_input_test.go | TestReviewerReadsReservedInputAcrossRepairs |
| Recover an older reviewer reservation | scenarios/harness/reviewer_input_test.go | TestLegacyReviewReservationRemainsUsable |
| Reject an assertion executed on changed raw files | scenarios/harness/verification_candidate_test.go | TestVerificationRejectsIntermediateRawChanges |
| Recover from a repository check changing the candidate | scenarios/harness/verification_candidate_test.go | TestDeclaredRunRejectsChangedRawInputs |
| Resume a failed repository check | scenarios/harness/repository_retry_test.go | TestResumeAndReplayRepositoryCheckRetry |
| Replay a completed commit while publication is pending | scenarios/harness/commit_history_test.go | TestCommitReceiptCanBeReplayedWhilePublicationIsPending |
| Correct an incomplete implementation | scenarios/harness/implementation_retry_test.go | TestIncompleteImplementationReturnsToItsOwner |
| Correct an incomplete implementation | scenarios/harness/implementation_retry_test.go | TestImplementationCheckCanBeRetriedBeforeEditing |
| Correct an incomplete implementation | scenarios/harness/implementation_retry_test.go | TestImplementationCorrectsDeclaredCheckAfterPassingBehavior |
| Recover a failed assertion after its base moves | scenarios/harness/implementation_retry_test.go | TestFailedAssertionRechecksUnchangedSourceAfterBaseMoves |
| Recover after repository retry evidence expires | scenarios/harness/repository_retry_test.go | TestCompletedRepositoryRetryTicketExpiresWithEvidence |
| Retain the failure that led to a source repair | scenarios/harness/repository_retry_test.go | TestRepositoryCheckFailureSurvivesSourceRepair |
| Reverify earlier daily evidence | scenarios/harness/verification_candidate_test.go | TestLegacyVerificationGetsFreshPerRunEvidence |
| Staging changes after an assertion | scenarios/harness/verification_index_test.go | TestVerifyStagedContentsAfterIndexOnlyChange |
| Diagnose an unstable check before repair | scenarios/harness/verification_candidate_test.go | TestInputChangeDoesNotDispatchABehaviorRepair |
| Preserve a completed commit after checkout changes | scenarios/harness/verification_candidate_test.go | TestCompletedDeliveryKeepsItsCommitAfterLogsAndHistoryChange |
| Preserve a completed commit after checkout changes | scenarios/harness/commit_history_test.go | TestLegacyCommitDeliveryStaysComplete |
| Repeat the behavior that required repair | scenarios/harness/assertion_recovery_test.go | TestRepairCannotSilentlyReplaceItsFailingAssertion |
| Correct a mistaken assertion | scenarios/harness/assertion_recovery_test.go | TestAssertionCorrectionRetainsOriginalFailure |
| Reconcile an outstanding reviewer after rollback | scenarios/harness/claim_recovery_test.go | TestOutstandingReviewerSurvivesCandidateRollback |
| Recover the linked native reviewer | scenarios/harness/claim_recovery_test.go | TestRecoverExactWorkerAcrossReplacementReservations |
| Recheck a completed publication | scenarios/harness/publication_recheck_test.go | TestReconfirmPublishedBranchAfterOneDestinationChanges |
| Confirm a PR from a mirrored push destination | scenarios/harness/publication_recheck_test.go | TestPullRequestOnSecondPushDestinationCanBeRechecked |
| Set up the selected host from an offline artifact | scenarios/installers/artifact_install_test.go | TestReleaseArtifactsInstallMatchedPlugin |
| Install the CLI and its matching host instructions | scenarios/installers/marketplace_test.go | TestMatchedPluginArtifact |
| The checkout switches branches before delivery | scenarios/harness/commit_target_test.go | TestCommitTicketAfterCheckoutSwitch |
| Conditional commit after a failed verification | scenarios/harness/commit_authority_test.go | TestConditionalCommitAuthorityPreservesFailedCandidate |
| Recover a commit after a branch switch | scenarios/harness/commit_target_test.go | TestResumeDoesNotOfferDuplicateCommitOnDifferentBranch |
| Identify and resume a daily task | scenarios/harness/resume_context_test.go | TestIdentifyAndResumeDailyTask |
| Recover an unaffected task from an incomplete inventory | scenarios/harness/resume_context_test.go | TestTaskListKeepsHealthyRowsWhenADailyRecordIsUnreadable |
| Resume while a managed worker owns the worktree | internal/cli/onboarding_terminal_linux_test.go | TestResumeDuringManagedIntervalDoesNotLaunchDailyHost |
| Start the host in the intended worktree | internal/cli/onboarding_terminal_linux_test.go | TestInteractiveResumeRequiresOriginalWorktree |
| Explain a source-changing check before the first commit | scenarios/harness/verification_output_test.go | TestFirstCommitRecoversWhenPassingAssertionCreatesBuildOutput |
| Choose the starting activity and finish | internal/cli/ux_help_test.go | TestTaskScopeChoicesAreDiscoverableAndValidatedBeforeOpeningRepository |
| Correct an implementation after a reviewed candidate fails | scenarios/harness/review_policy_test.go | TestReviewedImplementationRecoversAfterFailedBehavior |
| Correct findings on an implementation before testing | scenarios/harness/review_finding_recovery_test.go | TestReviewFindingOnOwnImplementation |
| Reassess a staged finding before repair on a broader candidate | scenarios/harness/review_finding_recovery_test.go | TestStagedReviewFindingNeedsWholeCandidateAfterExtension |
| Resume a host without reading unrelated usage | scenarios/harness/resume_context_test.go | TestTerminalResumeCarriesScopeAndWorkerRecoveryWithoutUsage |
| Incomplete detachment | scenarios/pushgate/removal_test.go | TestAnotherToolsHookStillCallsCw |
| Resume and deliver the same ticket | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Repair a reproduced failure and verify the commit | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Withhold an unsupported receipt | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Import a public ticket link and reopen its Markdown offline | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Reject a non-public direct destination | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Classify address ranges | internal/tickets/transport_test.go | TestPublicTicketAddress |
| Connect only to validated IPs | internal/tickets/transport_test.go | TestPublicTicketDialUsesOnlyValidatedLiterals |
| Validate each redirect destination and use a direct connection | internal/tickets/transport_test.go | TestPublicTicketChecksEveryRedirect |
| Verify the original request hostname over TLS with a pinned connection | internal/tickets/transport_test.go | TestPublicTicketUsesOriginalTLSIdentity |
| Deliver a ticket supplied by a host tracker | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Fetch and validate public ticket content | internal/tickets/public_test.go | TestPublicTicket |
| Share a verified patch or exact commit | scenarios/harness/shareable_receipt_test.go | TestMarkdownReceiptExportsOnlyTheSavedVerifiedPatch |
| Share a verified patch or exact commit | scenarios/harness/shareable_receipt_test.go | TestMarkdownReceiptDistinguishesVerificationOrderAndLegacyCommit |
| Share a verified patch or exact commit | scenarios/harness/native_review_test.go | TestRequiredReviewNeedsNativeWorkerOnCurrentCandidate |
| Share a ticket-linked result without private details | scenarios/harness/bug_delivery_test.go | TestTicketLinkedDelivery |
| Withhold Markdown before verification succeeds | scenarios/harness/shareable_receipt_test.go | TestMarkdownReceiptRequiresSavedSuccessfulVerification |
| Withhold Markdown before verification succeeds | scenarios/harness/shareable_receipt_test.go | TestMarkdownReceiptRejectsFailedRepositoryCheck |
| Start and resume repair from a selected check | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckReadsMetadataOnce |
| Preserve the required check and review through delivery | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckReachesVerifiedCommit |
| Approve a local reproduction on an unchanged PR | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckFromPrivateLocalApproval |
| Approve a local reproduction on an unchanged PR | internal/cli/ux_check_trust_test.go | TestLocalCIApprovalRequiresDeveloperInUnconfiguredRepo |
| Finish or resume local delivery with one command | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckCommitStopsAtFailedGate |
| Finish or resume local delivery with one command | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckResumesAfterReceiptCheckFails |
| Retain the opening PR revision through delivery | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckKeepsTheOpeningPRHead |
| Keep reproduction pending without qualifying evidence | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckRejectsUnapprovedOrStaleInput |
| Keep reproduction pending without qualifying evidence | scenarios/harness/ci_delivery_test.go | TestFailedPRCheckWaitsForLocalReproduction |
| Keep reproduction pending without qualifying evidence | internal/tickets/pull_request_test.go | TestFailedCheckMetadataRequiresMatchingCompletedFailure |
| Canonicalize the source link | internal/tickets/pull_request_test.go | TestFailedCheckLocationRequiresStablePRLink |

Go, JavaScript and C++ fixture commands execute locally. Host judgments are scripted. Published release checks execute on native CI runners when a release is available. Interactive host quality, speed and token costs require separate observations.

## Failed PR check replay

Twelve paired Linux replays used fresh repositories, the same scripted repair and two declared checks. Each path produced a verified local commit and receipt on all twelve attempts. PR metadata was supplied locally and the separate review regression exercised the required review path.

| Intake | CLI calls through receipt | Median intake | Median through receipt | Median CLI output |
| --- | ---: | ---: | ---: | ---: |
| Ticket, intake record and reproduction | 7 | 135.8 ms | 452.9 ms | 5,020 bytes |
| Failed PR check intake | 5 | 81.4 ms | 403.7 ms | 4,346 bytes |

The timers covered local execution from intake through receipt after checkout and check approval. Host calls and network time were outside this replay. CLI output measures bytes.

### Finish in one command

Twelve fresh pairs compared the prior split delivery with the combined command, using the same PR intake, staged repair and two required repository checks. Both builds issued all twelve receipts. The checks compared each receipt with the original PR head, the actual Git commit and distinct before-commit and on-commit run records.

| Delivery | Calls from intake to saved receipt | Median through receipt | Median after repair was recorded |
| --- | ---: | ---: | ---: |
| Verify, commit, then issue receipt | 5 | 403.2 ms | 275.7 ms |
| Commit from verification with receipt | 3 | 330.0 ms | 202.5 ms |

The Linux amd64 installer accepted a locally built preview archive and its matching plugin through the local download and offline paths. Both native host clients registered the plugin and the checks confirmed skill and hook discovery. The failed PR check scenarios also passed using the installed binary. Runtime hook observations and model token usage require signed-in host runs.

### Approve a reproduction without changing the PR

A fresh Linux fixture repository with no declaration accepted an interactive private command approval. Intake used supplied PR metadata for the unchanged head. The delivery receipt named that head, the approved check digest and the verified commit. Git recorded only the repair file. Mixed-policy regression coverage also verifies required repository checks and observed review, and rejects an approval changed after verification.

### Resume a failed check on pflag

The offline replay used pflag base `f8a990678a6d44385313e9b19e40737f889f9b14` and the regression from the archived issue 537 repair. The PR fixture committed the reproducer without its repair and supplied a failed check record for that exact head. A terminal approval selected `go test -count=1 ./...`. The named regression failed on intake. After applying the archived `flag.go` repair, the suite passed before commit and again on the delivered commit. The receipt's tree matched the archived repair's tree and Git recorded only `flag.go` in the delivery.

Both Codex and Claude Code handoff previews named the same saved task. A fresh `task next --brief` recovered the PR, selected check, action ticket and failing log in 784 bytes. The full JSON for the same state was 3,733 bytes. This replay took 0.979 s at intake and 0.996 s for final delivery on Linux amd64 with Go 1.27.4. These timings are one local observation. Metadata, repair and handoffs were replayed locally.

### Read GitHub check runs and commit statuses

A read-only GitHub CLI check on Terraform PR 39571 observed 14 entries with both GitHub rollup types at head `704818fba161a7c723ce39351c0e22a5d041a304`. The projected response kept the same PR identity and all 14 type, name, context and state fields, reducing the payload from 4,199 to 1,898 bytes. Offline tests cover failed and errored commit statuses, pending and passing rows, and ambiguous mixed failures. The intake scenario uses a failed commit status and repeats the saved result without a second fetch. [GitHub CLI rollup query](https://github.com/cli/cli/blob/trunk/api/query_builder.go)

### Paired replay of real pflag delivery

Both builds used the same pflag regression, fixture head `77474e17c1a8652d41046d1b2c04f420610c64d5`, committed check, approved repair, and receipt criteria. Both recorded the named failure, passed the required Go suite before commit and again on the commit, and produced clean tree `7e4e7d15146a18ad3b08cd44698e0de015c203ca` with only `flag.go` changed from the fixture head. The old split command sequence took five invocations and the combined sequence took three. This preflight used a scripted repair and ran each arm once. Actual model usage remains open.

A subsequent replay advanced the PR branch with an empty commit after reproduction. Kitsu withheld the repair ticket and receipt. Restoring the original head recovered the saved failing run, and the repair delivered the same verified tree directly on top of that head.

### Compare builds from the same source

Both binaries were rebuilt from the same captured Kitsu source. The split build omitted only the dispatch into the combined CI delivery command. Both used the same approved check, unopened task and clean pflag checkout. The combined path used three Kitsu calls and emitted 2,552 bytes through the saved receipt. The split path used five calls and emitted 4,676 bytes. Each delivered the same tree and passed receipt checks against Git and the saved run logs. These figures cover one local pair.

The receipt checker rejected a changed PR head, a receipt for a different commit, an omitted repository check, reused pre-commit evidence and a log with a mismatched digest. An independent run on archived Git contents confirmed the named regression failed at the starting head and passed on both delivered commits.

### Signed-in comparison criteria

Require both paths to reproduce the same regression and deliver a scoped commit with passing required checks and the same review policy. Record wall time, developer interventions and captured tokens for the coordinator and every descendant, including failed attempts. Report incomplete attempts and missing usage separately from completed comparisons.

An offline Linux process test used a 2 million token budget with simulated counters. It stopped the supervised processes at 1.7 million captured tokens, counted each descendant once and treated delayed usage after shutdown as unknown. Signed-in execution and actual token usage remain pending.

The frozen two-attempt launcher also ran both scripted pflag deliveries inside isolated Linux workspaces. Both saved receipts on the same verified commit tree. The monitor counted a coordinator, worker and grandchild once per attempt. Missing initial usage evidence stopped the comparison before the second attempt. The signed-in entry refused both pending approval and altered inputs before creating a worktree or opening the provider connection.

### Signed-in pflag pair

On 20 November 2026, the split and combined paths each ran once on GPT-6.2 Sol with high reasoning. Both started at the same pflag fixture head and delivered a one-file local commit. An independent check validated both receipts against Git and the saved runs. It reran the named regression and the full Go suite on an export of each commit. Both passed on different commit trees. The fixture required its repository check and had no independent review requirement.

| Delivery | Host wall time | Captured tokens | Uncached input | Cached input | Output | Model sessions | Developer interventions during task |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Split | 125.04 s | 850,422 | 80,453 | 764,544 | 5,425 | 2 | 0 |
| Combined | 113.18 s | 820,622 | 111,911 | 704,000 | 4,711 | 3 | 0 |

The combined attempt took 9.5% less wall time and used 3.5% fewer captured tokens. Uncached input rose by 39.1%. The split run started one nested Codex process and the combined run started two. Their actual session IDs and finished counters were reconciled against the coordinator's command output, retained rollouts and budget ledger. This required operator work outside the timed tasks. The pair captured 1,671,044 tokens, below the 1.7M stop threshold. These measurements describe one ordered pair. Billing savings remain unverified.

## Feature acceptance

Run `go test ./scenarios/harness -run 'Test(VerifiedFeature|FeatureDraft|FeatureBrief)'`.
The export fixture checks empty JSON, quote round-trip and unchanged text bytes. It
also exercises withdrawn approval, a different brief and required native review
evidence before commit. Draft, named-check and replay cases use isolated Git
checkouts and recorded host events.


## Offline delivery comparison

| Case | Observed result |
| --- | --- |
| Shared daily skill at merged #259 (`3ca4097`) | 7,611 bytes |
| Shared daily skill after #261 (`90c599d`) | 5,678 bytes |
| Shared daily skill with Jev instructions | 5,866 bytes |
| Codex and Claude Code handoff fixtures | Both use the supplied verification command. An identical replay preserves the run ID. Changed staged content gets a new run. Both recover the delivered commit. |
| Jev settings disabled | No HTTP call for the feature fixture. |
| Jev settings enabled | One local TLS request for each labeled clear or ambiguous story. Six matching answers are saved. Replays and lost results add zero requests. |
| Jev outage or policy change | The fixture permits local work. Replays and stale approvals add zero requests. |

Byte counts come from the shipped skill, including its header. The Jev tests
use fixed responses and fixture token counters. A signed-in host trial would
measure captured input, cache, output, elapsed time and developer interventions
on matching tasks, with Jev disabled first and then enabled on the same build.
These local results establish the contract and give a prompt size comparison.
