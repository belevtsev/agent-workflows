# Verification

| Scenario | Test file | Text |
| --- | --- | --- |
| Continue an approved design | scenarios/harness/harness_test.go | TestJavaScriptPlanThenImplement |
| Review a named committed range | scenarios/harness/committed_review_test.go | TestCommittedReviewRange |
| Review an existing candidate | scenarios/harness/harness_test.go | TestCppReviewOnlyPreservesIndexAndWorkingTree |
| Repair and recover | scenarios/harness/harness_test.go | TestGoExistingFixRepairVerificationAndRecovery |
| Confirm publication | scenarios/harness/harness_test.go | TestPublishChecksActualBranchBeforeCompleting |
| Identify when the verified commit already existed | scenarios/harness/commit_history_test.go | TestDeliveryDistinguishesTheCommitAlreadyPresentAtVerification |
| A staging-only change reopens review | scenarios/harness/harness_test.go | TestStagedReviewChangesWithIndexOnly |
| Interrupted verification resumes | scenarios/harness/harness_test.go | TestInterruptedProofNeedsFinalObservation |
| Follow a brief action receipt | scenarios/harness/harness_test.go | TestBriefRetainsDecisionAndExpandsFullFindings |
| Stage the verified tree for commit | scenarios/harness/harness_test.go | TestVerifiedCommitTicketSurvivesStagingExactlyTheVerifiedTree |
| Recover a delegated implementation or repair | scenarios/harness/delegated_work_test.go | TestDelegatedWorkRecovery |
| Recover a delegated implementation or repair | scenarios/harness/delegated_work_test.go | TestLinkedDeveloperCanReturnAfterDeadline |
| Resolve reviewer settings at reservation | scenarios/harness/review_settings_test.go | TestReviewReservationResolvesHostSettingsOnce |
| Extend a partial review to delivery | scenarios/harness/harness_test.go | TestReviewScopeExtensionChecksTheWholeDeliveryCandidate |
| Pin limits before launching a host | internal/cli/onboarding_terminal_linux_test.go | TestInteractiveHarnessPinsScopeAndResumesOriginalTask |
| Bound independent review starts | scenarios/harness/review_limits_test.go | TestReviewDispatchLimitPreservesFindingsAndCanResume |
| Accept a completed reserved review | scenarios/harness/review_limits_test.go | TestReviewFinishingAfterDeadlineCanReturnCurrentResult |
| Reserve review with a result | scenarios/harness/review_limits_test.go | TestRecordAndClaimReviewSurvivesRetries |
| A commit has an unexpected merge parent | scenarios/harness/commit_history_test.go | TestUnreviewedMergeParentCannotCompleteDelivery |
| Finish a pending merge | scenarios/harness/commit_history_test.go | TestPendingMergeMustActuallyBeCommitted |
| A hook rewrites the verified tree | scenarios/harness/commit_history_test.go | TestCommitHookChangingIndexCannotRecordVerifiedDelivery |
| Merge parents change during verification | scenarios/harness/commit_history_test.go | TestChangingMergeParentsDuringVerificationRequiresAnotherAssertion |
| Brief verification names the executed check | scenarios/harness/verification_output_test.go | TestBriefVerificationPointsToExecutedLog |
| Stream full verification output | scenarios/harness/verification_output_test.go | TestFullVerificationStillStreamsDiagnostics |
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
| Diagnose an unstable check before repair | scenarios/harness/verification_candidate_test.go | TestInputChangeDoesNotDispatchABehaviorRepair |
| Recover verification on an already reviewed commit | scenarios/harness/verification_candidate_test.go | TestLosingVerificationAfterCommitKeepsIndependentReview |
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
| Resume while a managed worker owns the worktree | internal/cli/onboarding_terminal_linux_test.go | TestResumeDuringManagedIntervalDoesNotLaunchDailyHost |
| Incomplete detachment | scenarios/pushgate/removal_test.go | TestAnotherToolsHookStillCallsCw |

Go, JavaScript and C++ fixture commands execute locally. Host judgments are scripted. Published release checks execute on native CI runners when a release is available. Interactive host quality, speed and token costs require separate observations.
