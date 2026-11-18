# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
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

Go, JavaScript and C++ fixture commands execute locally. Host judgments are scripted. Published release checks execute on native CI runners when a release is available. Interactive host quality, speed and token costs require separate observations.
