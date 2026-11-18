# Coding task harness

## ADDED Requirements

### Requirement: Start at the developer's current stage

The harness SHALL persist the requested outcome and working candidate and support planning, implementation, review, verification and delivery. Scope extension MUST be explicit.

Source: proposal.md and approved daily coding harness plan.

#### Scenario: Continue an approved design

- **WHEN** a plan ends and the developer extends it to implementation and verification
- **THEN** the harness preserves the design and resumes at implementation.

#### Scenario: Correct an incomplete implementation

- **WHEN** the developer requests implementation and the submitted work fails its behavior assertion or a declared check
- **THEN** the next action permits an implementation correction within the original scope. A transient failure on the unchanged candidate can be retried before claiming the correction. The changed implementation must be recorded and verified before the task can finish.

#### Scenario: Recover a failed assertion after its base moves

- **WHEN** a failed candidate keeps the same source bytes but its Git parents change
- **THEN** the harness requires current verification or a fresh review as applicable before asking for a source correction. A previous correction ticket cannot authorize that verification.

#### Scenario: Review an existing candidate

- **WHEN** review-only is requested on a dirty checkout
- **THEN** a candidate-bound independent review can finish with findings while preserving the index and working edits.

#### Scenario: Review a named committed range

- **WHEN** a developer requests a read-only review between two named Git revisions
- **THEN** the task resolves and retains both endpoints. The independent reviewer's brief identifies the commits and how to inspect their changes. A fresh session recovers the original comparison after either name moves. Reviewing newer endpoints requires a new scope.

### Requirement: Verify and reconcile delivery

The harness SHALL record actual assertion execution and required repository checks, invalidate stale evidence, reserve independent reviews, and reconcile an interrupted commit before retry. When the developer requests publication, the harness SHALL check the remote branch and any requested open PR against the verified commit before reporting delivery. Hook detachment SHALL preserve evidence unless the developer explicitly requests a purge and detachment completes.

Source: proposal.md and approved daily coding harness plan.

#### Scenario: Repair and recover

- **WHEN** a reviewer identifies a defect in an existing fix
- **THEN** authorized repair leads to a fresh review and a real behavioral check, and a matching committed candidate can be recovered after interruption.

#### Scenario: Incomplete detachment

- **WHEN** a foreign hook still invokes Kitsu during an explicit purge
- **THEN** the hook and evidence remain available for recovery.

#### Scenario: Confirm publication

- **WHEN** verification succeeds and the developer requests publication to a particular branch
- **THEN** delivery remains open until that remote branch advertises the verified commit, and extending delivery to include a pull request requires an open PR for the requested head and base.

#### Scenario: Replay a completed commit while publication is pending

- **WHEN** Git already created the verified commit and the coordinator retries the original commit ticket before publication
- **THEN** the CLI reports the current delivery state and preserves the publication ticket. If Git finished before a commit result was recorded, the exact replay saves the observed commit ID on the original test event. The operation creates no further commit and attributes no new action to a worker.

#### Scenario: Identify when the verified commit already existed

- **WHEN** the current executable proof matches a delivered commit
- **THEN** the receipt distinguishes verification on that existing commit from subsequently observing a commit on the verified parents. A recovered commit is never duplicated to change the order of those events.

#### Scenario: A staging-only change reopens review

- **WHEN** the index changes after a review-only result and the working files keep the same contents
- **THEN** the recorded review no longer completes the task, including after extending its scope.

#### Scenario: Interrupted verification resumes

- **WHEN** assertion receipts exist and the final verification observation was interrupted
- **THEN** the task requires verification and can resume from still-valid executable evidence.

#### Scenario: Follow a brief action receipt

- **WHEN** an agent requests a brief receipt for a completed review with findings
- **THEN** it can identify the result and retrieve the actual findings from the full task.

#### Scenario: Stage the verified tree for commit

- **WHEN** the developer stages the entire verified tree after receiving its authorized commit ticket
- **THEN** the same ticket permits the commit while the verified source and parent match, and a partial index is rejected.

#### Scenario: Recover a delegated implementation or repair

- **WHEN** the host delegates an implement or repair action
- **THEN** one reservation retains its scope, starting candidate, host model, reasoning, skill references, owner and native launch label. The host may link one worker ID and accept a result from that worker on the assigned base. Expected source edits are allowed, and a changed base requires reconciliation. The reservation and accepted result survive restart. A linked worker may return its result after the deadline without starting another worker or extending the deadline. Direct coding and independent review remain available.

#### Scenario: Resolve reviewer settings at reservation

- **WHEN** a coordinator reserves a review for a specific host
- **THEN** the reservation and review record retain that host's selected model, reasoning effort and skill source references, and resolution failure leaves the action unclaimed.

#### Scenario: Extend a partial review to delivery

- **WHEN** the requested delivery contains files outside a previously reviewed index or the review base changes before verification
- **THEN** the whole delivery candidate requires a fresh review, a repeat reservation identifies the changed base and the diff since the previous review, and committing the verified tree onto the reviewed parent preserves that review.

#### Scenario: Bound independent review starts

- **WHEN** a configured total review-start limit or finish-time reserve is reached before an unclaimed review
- **THEN** the task pauses with the pending action, candidate, recovery command and access to the findings. Releasing a review consumes its slot, and explicitly extending the limit can resume review.

#### Scenario: Accept a completed reserved review

- **WHEN** a reserved review returns after its deadline
- **THEN** the result can be recorded against its original ticket and unchanged candidate, and further work remains subject to the deadline.

#### Scenario: Reserve review with a result

- **WHEN** recording intake, implementation or repair requests a reviewer on the next transition
- **THEN** the result and eligible reservation are saved together, applying the configured host and limits, and retrying reports current state without reserving another worker.

#### Scenario: Pin limits before launching a host

- **WHEN** a developer starts a task with review or time limits from a working tree
- **THEN** the CLI persists the limits before host launch and binding the scope preserves them. The developer can explicitly extend the review allowance or time window, including before initial scope binding.

#### Scenario: A commit has an unexpected merge parent

- **WHEN** a new commit contains verified files and adds a parent outside the verified merge
- **THEN** delivery requires fresh verification.

#### Scenario: Finish a pending merge

- **WHEN** an authorized merge has the same tree as the current commit
- **THEN** delivery remains open until the verified tree and all expected parents appear in a commit.

#### Scenario: A hook rewrites the verified tree

- **WHEN** a normal Git hook changes the tree recorded by a requested commit
- **THEN** the command reports the mismatch and preserves the created commit for inspection.

#### Scenario: Merge parents change during verification

- **WHEN** the parent set changes during the assertion
- **THEN** the task requires a new assertion for the current merge.

#### Scenario: Brief verification names the executed check

- **WHEN** a task tests a candidate with `--brief`
- **THEN** child output is retained under the usual log limits, the receipt identifies the assertion or first unsuccessful declared run and its digest, and an unsuccessful run leaves verification incomplete.

#### Scenario: Stream full verification output

- **WHEN** the developer runs task verification with ordinary text or full JSON output
- **THEN** assertion and declared-check diagnostics stream live and the JSON result remains parseable.

#### Scenario: Read a reserved reviewer input

- **WHEN** a host forwards the review input command from an active review reservation
- **THEN** the reviewer can read the pinned candidate, request, intake, plan, latest work note, relevant prior verification failure, previous review findings and chosen settings in a read-only checkout. A changed live candidate invalidates the review return. Released and completed tickets cannot open this view.

#### Scenario: Recover an older reviewer reservation

- **WHEN** a saved active review predates pinned reviewer inputs
- **THEN** the host can recover it through the full task record and submit its result against the original ticket.

#### Scenario: Reject an assertion executed on changed raw files

- **WHEN** a behavioral assertion changes file bytes that normalize to the same Git tree, or changes the candidate's commit parents
- **THEN** its run cannot establish readiness, a later check cannot erase the mismatch, and restoring the original candidate requires a new assertion.

#### Scenario: Recover from a repository check changing the candidate

- **WHEN** an independently executed repository check changes the candidate before it returns
- **THEN** verification withholds completion and retains the actual outcome. A retry on the original candidate can reuse the stable behavioral assertion and must execute the affected repository check again.

#### Scenario: Resume a failed repository check

- **WHEN** a declared repository check fails after the behavioral assertion passes
- **THEN** a fresh resume returns the exact failed check and log receipt. A retry on the unchanged candidate reuses the valid passing evidence, runs the outstanding check, and retains the failed receipt in task history. Replaying a successful retry returns current state without executing another check.

#### Scenario: Recover after repository retry evidence expires

- **WHEN** a newer check result or a missing log invalidates evidence from a completed retry
- **THEN** the completed retry ticket cannot authorize another execution, and the task requires its current verification ticket.

#### Scenario: Retain the failure that led to a source repair

- **WHEN** a failed repository check leads to an authorized source repair and a new verification
- **THEN** the task verifies the changed candidate and retains the exact failed check receipt on the original assertion event.

#### Scenario: Reverify earlier daily evidence

- **WHEN** an earlier task has a passing receipt that predates per-run observation of original bytes and commit parents
- **THEN** the task keeps its history and requires fresh local evidence before reporting current verification.

#### Scenario: Diagnose an unstable check before repair

- **WHEN** a normally exiting command reports a failure after changing the original candidate
- **THEN** the task reports the input change, retains the child exit status and requests verification on stable inputs before dispatching a behavioral repair.

#### Scenario: Recover verification on an already reviewed commit

- **WHEN** a delivered candidate loses test evidence and its independent review still matches the original bytes, committed tree and expected parents
- **THEN** the task can reverify the existing commit without consuming another review slot. A different parent requires a fresh review.

#### Scenario: Repeat the behavior that required repair

- **WHEN** a behavioral assertion failed and a repaired candidate returns from review
- **THEN** the saved command can be rerun with its current ticket. A different command cannot silently displace the failed assertion even when the repository suite passes.

#### Scenario: Correct a mistaken assertion

- **WHEN** the verifier explicitly explains a different command for a failed assertion
- **THEN** the new test event identifies the previous command, expectation, failed run and replacement reason. An inconclusive attempt retains the unresolved failure. An unclaimed repair can retest the unchanged candidate. A claimed repair or changed candidate continues through its existing workflow.

#### Scenario: Reconcile an outstanding reviewer after rollback

- **WHEN** an active reservation no longer matches the candidate or next action, including rollback to an already approved candidate
- **THEN** the harness returns reconciliation with the saved owner and ticket and withholds new action tickets. After the worker is reconciled and released, current evidence determines the next action. A review still matching its ticket may return after its deadline.

#### Scenario: Recover the linked native reviewer

- **WHEN** the coordinator links a launched native reviewer to its existing reservation
- **THEN** the saved identity is available on resume. Repeating the link consumes no new reservation. A different worker cannot overwrite that identity or submit the review result for it. A released ticket cannot bind a replacement reservation.

#### Scenario: Recheck a completed publication

- **WHEN** a developer confirms a previously delivered task again
- **THEN** confirmation reads every current push destination and the requested PR. Failure reopens publication while retaining the local verification, commit and historical confirmation. Local resume can show the last observation without contacting a remote.

#### Scenario: Confirm a PR from a mirrored push destination

- **WHEN** the requested branch is present on every push destination and the open PR matches the commit, head branch, base branch and one configured source repository
- **THEN** confirmation succeeds regardless of destination order. A PR from another fork or a closed PR cannot establish delivery.

#### Scenario: Install the CLI and its matching host instructions

- **WHEN** a developer installs a daily-harness build from platform artifacts
- **THEN** the same build supplies a checksummed local marketplace for Codex and Claude Code. The installed task skill, role definitions and hooks match the CLI source revision and work without a source checkout.

#### Scenario: Set up the selected host from an offline artifact

- **WHEN** a developer runs the bundled installer with a local archive directory and a supported host
- **THEN** it verifies and installs the native CLI and uses that installed executable to verify the plugin from the same artifact and register it with the chosen host. A single matching native tag can be selected from the manifest. Ambiguous tags require an explicit selection.
- **AND** the selected host can use the installed plugin after the downloaded artifacts are removed. Repeating setup is supported. Failed host registration reports how to retry and cannot count as a successful setup.

#### Scenario: The checkout switches branches before delivery

- **WHEN** a commit ticket was issued on one branch and the developer switches to another branch or detached HEAD at the same revision
- **THEN** a new commit-scoped task withholds action tickets on another checkout and identifies the recorded and current destinations. Returning to the intended checkout can reuse valid review and assertion evidence. A check that switches branches cannot establish readiness there. On recovery, a commit observed elsewhere cannot satisfy a recorded local delivery destination unless that destination also points to the verified commit.

#### Scenario: Conditional commit after a failed verification

- **WHEN** the developer authorizes a commit only if the supplied patch passes and its behavioral assertion fails
- **THEN** the verify-entry task without repair authority retains the failed assertion and offers verification again. It provides no repair or commit ticket. Reporting the failed condition preserves the candidate, index and branch.

#### Scenario: Recover a commit after a branch switch

- **WHEN** Git has already committed to the recorded branch but the checkout moves to another branch at the old parent before the coordinator resumes
- **THEN** the receipt identifies the verified commit on the intended branch and withholds a duplicate commit ticket. On return to the intended branch the same task can finish using the recorded verification.

#### Scenario: Identify and resume a daily task

- **WHEN** a developer chooses among saved daily tasks after a session interruption
- **THEN** the list identifies requests with bounded single-line excerpts and the last recorded action. The full next-action view recovers the request, scope, latest result, relevant earlier intake, plan and work notes, and whether the most recent review applies to the live candidate. The default terminal view identifies findings from an earlier candidate and directs the developer to the saved review. Routine brief responses omit those earlier notes.

#### Scenario: Resume while a managed worker owns the worktree

- **WHEN** a daily task is unfinished and a managed interval is active in the same worktree
- **THEN** task inspection identifies the interval, retains the pending daily action and withholds its ticket. An interactive daily resume refuses to launch the host until the managed interval is finished.
