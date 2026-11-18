# Can Kitsu carry everyday coding work to a verified finish?

18 November 2026. Source under review was the daily harness on `codex/task-harness`, based on `e9bfb417` with local changes. The paid comparison used a frozen Linux binary. Later fixes and measurements are identified separately below.

**Decision.** Use the daily harness where a developer needs to resume work, preserve a review, establish which tree passed, or authorize a delivery. Plain Codex used 394,607 tokens in 154 seconds on the pflag task. Daily Kitsu used 841,548 in 187 seconds. Both patches met the frozen acceptance criteria. The extra reviewer produced no demonstrated quality advantage on this task. On the qs review-and-repair task, later reviews produced tests that failed on earlier repairs. The Kitsu arm exhausted the shared stop threshold before the requested commit. A developer evaluating the daily harness can install the matching preview CLI and plugin from artifacts. [Daily how-to](../task-harness.md) · [Installation and release procedure](../release-install.md)

## What happened on a real task

Two independent Codex sessions fixed upstream pflag’s handling of unknown flags during non-interspersed parsing. The requested behavior covered long flags, shorthand clusters, and both public parse APIs. Both attempts had GPT-6.2 Sol/high, the same application source, toolchain, acceptance tests and clean local commit requirement, with 12 minutes per attempt. Plain ran first and could choose its own workflow. Kitsu ran second with compact off and its normal independent review. Each solver had an isolated workspace. The runner mounted the daily skill and supplied its ID in the Kitsu prompt. Timing starts with the host invocation and excludes installation and interactive setup. The grader and its acceptance cases ran separately. The limits were 6M captured tokens across the two attempts, a 5M stop threshold and no replacement runs. Total captured was **1,236,155**. [Machine-readable results and frozen build identity](daily-review-data/pflag-pair.json)

| Observed metric | Plain Codex | Daily Kitsu |
| --- | ---: | ---: |
| Coordinator tokens | 394,607 | 745,088 |
| Worker tokens | 0 | 96,460 |
| **All actual input + output tokens** | **394,607** | **841,548** |
| Uncached input | 30,133 | 82,421 |
| Cached input | 358,912 | 751,744 |
| Output | 5,562 | 7,383 |
| Solver wall seconds | 154.01 | 186.54 |
| Local process CPU seconds | 68.38 | 27.19 |
| Local cgroup peak MiB | 607.3 | 340.0 |
| Clean committed candidate | Yes | Yes |
| Frozen acceptance criteria | 2/2 | 2/2 |
| Original suite and frozen patch integrity | Pass | Pass |
| Added regression against original / candidate | Fail / pass | Fail / pass |
| Supplementary callback scenario | Fail | Fail |

Actual tokens sum each unique session’s final counter. Cached input is already included. The local cgroup includes the client, tests and descendants and excludes provider compute. Solver wall time excludes setup and post-run grading. These are one observation per arm in fixed order on one machine. They cannot establish a population average, provider cost, or a speed change attributable to a particular fix.

**Where the extra work went.** Kitsu used 446,941 extra tokens. The coordinator accounted for 350,481 of that difference (78.4%). The reviewer accounted for 96,460 (21.6%). The plain coordinator made 13 host execution calls. The Kitsu coordinator made 21 plus one worker launch, including task transitions, a state refresh and a second commit call. The reviewer made three execution calls and returned its findings. Per-thread counts establish where usage accumulated. They cannot assign all of the coordinator difference to any single command. The verified-tree staging fix below removes one observed reason for a retry.

**Quality check beyond a green suite.** Both candidates added a new test to an existing test file. A conservative file-level detector flagged that file in both arms. A declaration-level comparison found every original test declaration intact. Each new regression failed on the original implementation and passed on its own candidate in isolated offline reruns. Both candidates also failed two cases about a callback changing parsing mode midway through a shorthand cluster. The upstream reference fix failed the same supplementary test. This identifies an unresolved compatibility question. A maintainer should decide the expected callback contract before using it to grade a release. The paid grader certifies the two frozen criteria. Worker presence and the saved review establish that a review occurred. Reviewer independence of judgment and exhaustive coverage remain unmeasured. [Detailed outcomes](daily-review-data/pflag-pair.json)

## Did the extra pflag review improve the regression suite?

Both paid candidates changed the same executable lines. Their `flag.go` differences were prose comments. The other differences between the two frozen trees were the submitted tests and two Kitsu configuration files. We ran both submitted regression tests against the **same production implementation**, then introduced five separate faults derived from the original request. An independent public-API probe detected all five and passed on the correct control. Every run used local Go with network module lookup disabled.

| Seeded fault | Original pflag suite | New plain Codex test | New Kitsu test |
| --- | --- | --- | --- |
| Drop arguments following the unknown flag | Missed | Caught | Caught |
| Continue parsing after the first unknown | Missed | Caught | Caught |
| Parse a known shorthand after the unknown in the same cluster | Missed | Caught | Caught |
| Forward the known suffix in interspersed mode | Caught | Missed | Caught |
| Stop early in interspersed mode | Caught | Missed | Caught |

**Both delivered suites caught 5/5 seeded faults and accepted the correct control.** The Kitsu test covered both interspersed regressions independently. The plain candidate inherited that coverage from the original suite. The additional 446,941 captured tokens on this paid task bought no extra detection on these controls. The mutations were selected after the run, and five related faults cannot establish general defect rates or isolate the reviewer's contribution. To reproduce, supply local Git repositories containing the two recorded candidate trees to `python3 docs/benchmarks/pflag-regression-strength.py --codex PATH --kitsu PATH`. [Per-control outcomes and tree IDs](daily-review-data/pflag-regression-strength.json) · [Offline driver and independent probe](pflag-regression-strength.py)

## Did orchestration produce a better qs patch?

We extracted production library code from the archived Git trees and tested it against the same unchanged qs suite and the same 19 public-behavior cases. Each candidate used the original package metadata, tests and dependencies. The corpus includes sparse-array holes, the overflow boundary and the scalar-append regression. The original and upstream fix calibrate the grader’s ability to distinguish this defect.

| Library under test | Unchanged suite assertions passed | Behavior cases passed |
| --- | ---: | ---: |
| Original qs | 1,686 | 2/19 |
| Upstream reference fix | 1,686 | 19/19 |
| Plain Codex, implementation task | 1,686 | 19/19 |
| Managed Kitsu, same implementation task | 1,686 | 19/19 |
| Daily Kitsu, separate partially fixed review task | 1,686 | 19/19 |

**The matched plain and managed candidates tied on the observed behavior.** The original suite passed even on the defective source, so its green result alone could not grade this change. The daily candidate came from a different starting patch, and its paid workflow stopped before delivery. Its row describes the approved code that survived that stop. Applying the common grader required zero new model requests. This comparison measures functional outcomes on one issue. It does not measure all defects, complexity, or how often a host will finish unaided. [Frozen tree identities, grader digest and results](daily-review-data/qs-archived-quality.json) · [Offline comparison](qs-archived-quality.py)

## From install to delivery

| Area | What a developer gets | Weak point or boundary |
| --- | --- | --- |
| Installation and host onboarding | Checksummed binary installers for macOS/Linux and Windows, plugin setup, host/hook diagnostics and a task launcher. | Public v0.0.1 predates the daily workflow. The preview passed all 16 native installation combinations across Linux, macOS and Windows. Interactive host permissions still require user action. [Install guide](../release-install.md) |
| Task entry and resume | Start with an idea, approved plan, partial fix, existing diff, or a candidate ready for verification. Stop after the requested outcome. Restore the request, current candidate, open findings and next action from the worktree ledger. | A review can finish with findings. The record is local to the worktree. Plan and implementation notes still depend on the host's judgment. [Daily harness](../task-harness.md) |
| Managed orchestration | Coordinator and specialized roles, scoped assignments, dispatch, returns, write boundaries, and OpenSpec artifacts for teams needing a durable shared change. | More setup and host interactions. Published task artifacts and the local daily ledger have different lifetimes. No crossover threshold for selecting the managed path has been validated. [Managed workflow](../workflow.md) |
| Models, reasoning, skills and advisers | Per-role host configuration, discovery and selection of original repository/user skills, optional consultations with retained answers and decisions. | Selection can be inspected. Whether the model applied the selected guidance correctly requires review of its work. Optional consultations add worker cost. [Roles](../roles.md) · [Skills](../skills.md) · [Advisers](../advisers.md) |
| Behavioral verification and repository checks | Run a concrete behavior assertion and declared checks, retain logs, bind evidence to content and recover after interruptions. Diff-sensitive checks and verifier calibration exercise known defects and controls. | A green command can assert the wrong behavior. Tests selected by the same model that wrote the patch leave blind spots. [Verification](../verification.md) · [Evidence](../evidence.md) |
| Git delivery and hooks | Protect unrelated edits, run normal commit hooks and confirm explicitly requested publication. Detach supporting hooks while retaining evidence or explicitly purge it. | Remote branch and PR facts are point-in-time observations. Delivery still depends on remote access and host credentials. [Hooks](../hooks.md) · [Delivery](../task-harness.md#delivery-and-limits) |
| Compact observations | Opt-in bounded collection, searches, check logs, references, freshness checks and recoverable expansion. Managed snapshots, deltas and briefs can avoid repeating state. | Expansion costs can exceed the original response. Some small reads grow. A lower byte count gives no guarantee about total host tokens. [Compact context](../compact-context.md) |
| Reporting and CLI | Per-task usage, coordinator/worker notices, workflow insights and budget metadata. Command help and completion are available across the CLI. | Interactive coordinator usage can be missing. A partial aggregate needs its coverage label. The command reference has 86 entries, a discovery burden for someone who wants to start one task. [Command reference](../cli-reference.md) · [Workflow insights](../workflow-insights.md) |

## Defects fixed during review

| Reproduction | Change and regression evidence |
| --- | --- |
| Complete a staged review, then change the index while leaving file contents alone. | Review-only completion tracks the index, HEAD and working content. A later scope extension preserves the original review boundary. [Harness journeys](../../scenarios/harness/harness_test.go) |
| Interrupt verification after saving run references but before recording its final observation, or move HEAD before committing. | Verification requires a final observation. A moved parent leads back to verification. Recovery can reuse still-valid assertion evidence. [Harness journeys](../../scenarios/harness/harness_test.go) |
| Start an ordinary interactive task with compact enabled. | The launcher pins the task before handing it off, including for opt-in compact observation. [Terminal regression](../../internal/cli/onboarding_terminal_linux_test.go) |
| Confirm publication with diverging push destinations, or an open PR on a different head repository with matching branch and hash. | Check every configured push URL, the PR head owner/repo and the saved confirmation on replay. Host matching supports an explicit HTTPS enterprise port. [Git checks](../../internal/gitx/harness_commit.go) · [Publication tests](../../internal/gitx/harness_commit_test.go) |
| Stage the already verified files and use the returned commit ticket, as the paid coordinator did. | Reproduced the ticket rejection before fixing it. The ticket now names the verified tree and parent. A partial index still fails. [End-to-end regression](../../scenarios/harness/harness_test.go) |
| List 20 managed tasks with verification disabled for the ordinary records. | Reused the common Git observation and policy. On the same checkout, the old and new binaries returned identical list JSON while Git trace counts of `write-tree` fell from 21 to 1. [Regression](../../scenarios/cliux/cliux_test.go) |
| Read token records in a path containing square brackets, or lose the coordinator notice while retaining a worker total. | Literal directory enumeration preserves the records. The task reports attributable usage and its coverage. [Token store tests](../../internal/tokens/tokens_test.go) · [Partial totals](../../internal/cli/harness_usage_test.go) |
| Install in a user directory outside PATH and try the CLI in the same terminal. | The POSIX installer now prints a shell-quoted export, including for a path with spaces or apostrophes. [Installer tests](../../scenarios/installers/installers_test.go) |

## What can be made cheaper offline?

The paid build predates the brief response, commit-ticket correction, task-list optimization and token-reporting changes. The following replays exercise the local implementation with no model calls.

A scripted Go change added a failing regression, changed the code, verified it, and produced the same clean committed tree through all three paths. Ten measured repetitions per arm rotated order after warm-up. On this shared machine the median plain shell route took 0.336 s, five commands and 422 response bytes. Kitsu full took 0.549 s, ten commands and 13,751 bytes. Kitsu brief took 0.548 s, ten commands and 2,787 bytes. **Brief removed approximately 80% of response bytes. It did not remove the additional five commands.** This measures local process overhead with scripted judgments. [Replay](daily-review-overhead.py) · [Per-run data](daily-review-data/daily-cli.csv)

The existing offline compact benchmark also ran ten repetitions per arm. Median response bytes for noisy checks fell from 193,785 to 6,460 with required facts preserved in the fixture. Exploration grew from 5,933 to 10,137 bytes after two expansions. Handoff grew from 5,619 to 7,645 bytes while calls fell from six to two. Compact has a demonstrated use in noisy check output. Use ordinary reads for small files. Older paid managed comparisons on pflag observed both a 5.5% decrease and a 21.1% increase in actual tokens. This daily pair used compact off in both arms. [Offline measurements](daily-review-data/compact-replay.csv) · [First paid comparison](compact-delivery-paid-report.md) · [Later paid comparison](workflow-insights-paid-report.md)

## Follow-up engineering and offline evidence

**Review an existing diff, then extend it to delivery.** A staged review could satisfy a delivery review even when an untracked file or unstaged edit was outside the reviewed index. A base commit could also change with the final file bytes intact. Both situations now require a fresh review. Committing the verified tree onto its reviewed parent preserves the result. The regression goes through reservation, review, extension, a concurrent base change, renewed review, verification and commit. [Developer journey](../../scenarios/harness/harness_test.go)

**Configure the reviewer during reservation.** On both hosts, `task claim --host HOST --brief` resolves the effective reviewer model, reasoning effort and original skill identities before saving the claim. Failed skill resolution leaves the action available. The completed review retains its requested settings. Brief mutations skip the unrelated token-history scan. On a Go implementation replay with a selected skill, separate role/skill lookups and a claim took 12 commands end to end, with a median 0.543 s and 3,717 response bytes. Requesting settings in the reservation took 10 commands, 0.533 s and 3,240 bytes. Plain shell took five commands, 0.326 s and 422 bytes. All three paths produced the same verified source tree. These are local process observations from ten runs per arm. [Measurements](daily-review-data/reviewer-reservation.csv) · [Replay with `--review-settings`](daily-review-overhead.py)

**Expand a long compact observation.** The pager formerly encoded every admitted line together with the entire preceding response. It now measures each proposed line once and encodes the completed page. On one Linux machine the median time to serve a 2,048-line read fell from 514 ms to about 3 ms. Allocated bytes per page fell from about 125 MB to 1.8 MB. The count of small allocations rose from about 12,500 to 20,300. An independent encoder checks the exact returned bytes across multiple items, byte limits and continuation offsets. The improvement applies to local page generation. The model receives the same content. [Timing samples](daily-review-data/compact-pager.csv) · [Regression and benchmark](../../internal/contextpack/page_benchmark_test.go)

**Verify an existing JavaScript fix.** We ran plain shell and the Kitsu verify-only path on three candidates from the real `ljharb/qs` sparse-array issue, using the same offline Node toolchain and checks. All three passed the unchanged original suite. The original source passed 2/18 existing acceptance assertions. A partially applied fix passed 18/18. A supplementary assertion for appending a scalar to a sparse array rejected both the original and partial sources, and accepted the full upstream fix. Kitsu returned `verify`, `verify` and `done` respectively. Plain shell returned failing, failing and passing exit codes. Comparing the outputs confirms that Kitsu's readiness decision honored the assertion. It also identifies a hole in the earlier grader. This replay used zero model calls and gives no estimate of model token usage. [Verified controls](daily-review-data/qs-verification.json) · [Offline replay](daily-verify-qs.py)

## Paid follow-up starting from a partially fixed qs patch

**The comparison stopped during its first arm.** The exported Kitsu patch passed all 19 frozen behavior assertions, the original suite and grader integrity checks. Kitsu recorded three repairs. The fourth independent reviewer approved the final patch 117 milliseconds before the stop. The task record had no final verification or requested commit. The monitor stopped the run at **5,063,787 captured tokens** after **658.51 seconds**. A forced stop can leave usage unreported. These tokens are a lower bound. Plain Codex had no attempt, time or token observation in this experiment. [Frozen build identity, per-session usage and quality](daily-review-data/qs-paid.json)

Both planned arms used the same seeded upstream source, shared review/repair/verify/commit request, protected grader and GPT-6.2 Sol/high configuration. Compact was off. Kitsu went first to reverse the order of the pflag pair. The user authorized 6M captured tokens, a 5M stop threshold, a 1M delayed-accounting reserve and 12 minutes per attempt. Accounting updates are discrete. The threshold ruled out the second launch. There were no paid retries, probe or separate judging session.

| Session | Captured tokens |
| --- | ---: |
| Coordinator | 3,762,842 |
| First reviewer | 335,263 |
| Second reviewer | 544,540 |
| Third reviewer | 303,160 |
| Fourth reviewer | 117,982 |

The first three reviews returned five, three and one findings respectively. The coordinator used 74.3% of the observed total. The model counters contained 273,426 uncached input tokens, 4,756,224 cached input tokens and 34,137 output tokens. The coordinator accounted for the largest share of usage across the four review rounds.

**Did the extra review find anything?** After the first review, we derived 12 exploratory checks from three reported cases: a scalar before an indexed value, mixed named/numeric keys and a sparse input object. Both the starting candidate and upstream reference passed 2/12. The exported Kitsu candidate passed 12/12. This is evidence of broader behavior coverage on that patch. Those cases were selected from Kitsu's findings, which limits their use for judging a competing workflow. The submitted JavaScript tests kept all original test-file lines in order, passed 1,726 assertions on the candidate and reported nine failures on the partial source before an exception terminated the replay. The file-level integrity detector flagged both edited test files. [Quality readback](daily-review-data/qs-paid.json)

**Which repairs changed observable behavior?** We exported each recorded Git tree and ran the same 19 frozen assertions, the 12 exploratory checks and the final submitted JavaScript tests against its implementation. The last column uses identical test files at every stage. Tokens at each event are the cumulative session counters available by its timestamp. Accounting can lag.

| Implementation tested | Elapsed | Captured tokens at event | Frozen checks | Exploratory checks | Final submitted suite on this source |
| --- | ---: | ---: | ---: | ---: | --- |
| Incoming patch | 31 s | 86,231 | 18/19 | 2/12 | 9 failures before early exit |
| First repair | 290 s | 1,218,992 | 19/19 | 12/12 | 1 failure before early exit |
| Second repair | 476 s | 3,030,872 | 19/19 | 12/12 | 1,724 passed, 2 failed |
| Third repair | 612 s | 4,315,187 | 19/19 | 12/12 | 1,726 passed |

The frozen grader would have accepted the first repair. The final suite distinguishes all three repairs and shows a concrete risk in ending review after the first green grader result. These additional assertions came from the Kitsu run. The plain arm has no corresponding result. [Saved trees and replay results](daily-review-data/qs-paid.json)

**Changes after the frozen build.** A later review reservation now provides the diff between the previously reviewed and current working trees and indicates a changed base. The host instructions ask a fresh reviewer to check earlier findings, repairs and affected behavior, recording adjacent suggestions in the review note. Changed review scope or base calls for inspection of the full candidate. The staged-review-to-delivery regression exercises the new receipt. Its effect on live tokens and completion remains unmeasured.

## What changed after the qs budget stop?

The qs coordinator made 41 host execution calls. Four contained a standalone `kitsu task claim`. The daily harness can now record intake, implementation or repair and reserve the next independent review in the same transaction using `--claim-review codex` or `claude`. It emits a new-claim receipt once. Replaying an earlier result recovers the current state and preserves a released reservation. The same configured reviewer, original skills and independent approval still apply.

We ran ten offline repetitions per arm after warm-up, rotating the order. The Go task reproduced a broken label normalizer, added a failing regression, fixed the implementation, exercised the assertion and committed. Every arm produced the same clean Git tree. Kitsu's reviewer was scripted. The plain path used shell and Git.

| Offline path | Commands | Response bytes | Median wall time |
| --- | ---: | ---: | ---: |
| Plain shell | 5 | 422 | 0.337 s |
| Kitsu with a separate review reservation | 10 | 3,240 | 0.551 s |
| Kitsu with the reservation in the implementation result | 9 | 2,974 | 0.536 s |

The combined path removed one command and 266 response bytes per review handoff on this task. Both Kitsu paths took longer than plain shell. No model ran in the replay. Live coordinator tokens for the new path remain unmeasured. [Per-run measurements](daily-review-data/review-with-result.csv) · [Replay with `--claim-with-result`](daily-review-overhead.py)

The daily harness also accepts a total review-start limit and a time reserve for finishing. When an unclaimed review reaches either boundary, it retains the candidate, pending action, findings reference and resume command. Released reservations consume a slot. A current result from a previously claimed reviewer can be recorded after the deadline. Starting a new check or commit still requires time. Limits apply at reservation time. Host cancellation and provider token accounting remain separate.

Applying an illustrative three-start limit to the recorded qs sequence places the pause immediately after the third repair, at about 612 s and 4.32M tokens observed by that event. The patch already passed the final suite there. Its independent approval came later, so a pause at that point would have produced a resumable review handoff. This historical comparison does not give a measured token saving or a completed delivery. [Repair snapshots](daily-review-data/qs-paid.json) · [Budget and retry regressions](../../scenarios/harness/review_limits_test.go)

## Native host discovery and Git delivery

On Linux x86-64, the POSIX installer installed a locally assembled, checksummed archive into a fresh home. The installed binary matched the supplied build byte for byte. We added the local marketplace and installed Kitsu using the native Claude Code 2.1.325 and Codex 0.169.0 commands. The replay ran in a namespace with external networking disabled, no developer credentials and no model request.

| Operation | Observed result |
| --- | --- |
| Claude initialization | The host returned both Kitsu skills and all seven role names. Its SessionStart event executed the Kitsu hook, emitted the task invocation and exited successfully. |
| Codex discovery | The app server listed both enabled Kitsu skills and all four hook events. All four hooks in this fresh account carried the untrusted status. A developer must review their hashes before relying on hook execution. |
| Task launch preview | Each host passed the local Kitsu setup check and received a handoff preview containing the requested verification outcome. |
| Installed CLI delivery | A scripted driver supplied intake for a pre-existing file. Kitsu ran the assertion, committed the verified tree and returned `done` after a fresh resume. Git reported a clean checkout. |

The original Claude catalog repeated dispatch instructions in every role description and carried a long description of the Git-gate skill. Shorter metadata retains the coordinator-brief requirement and the gate triggers. Across two fresh installations, `claude plugin details kitsu@kitsu` estimated **448** always-on tokens before the edit and **297** after it. Both installations exposed the same skills, role names and hook events. The 151-token difference is a host estimate for catalog text. Its effect on provider-billed usage and model routing quality remains unmeasured. [Native results](daily-review-data/native-host-setup.json) · [Replay](native-host-setup.py)

The delivery review reproduced three failures on the previous implementation. A first-parent match accepted an unexpected merge commit. A pending merge with an unchanged tree appeared finished before Git committed it. A hook could rewrite the index and the CLI returned success for the resulting commit. Verification now binds the working content and ordered merge parents. The commit command reads back Git’s actual tree and parents after running the hooks. Recovery also rejects changes to the merge parent set during verification. Each case has a CLI regression. [Commit history tests](../../scenarios/harness/commit_history_test.go)

On the same offline Go repair, ten runs per arm produced identical committed trees. Median wall times were 0.327 s for plain shell, 0.532 s with separate reviewer reservation and 0.509 s with the combined Kitsu receipt. This measures the whole scripted journey after the Git changes. [Post-fix timings](daily-review-data/commit-identity-replay.csv)

To repeat the native setup check on a Linux machine with Python 3.11+, Bubblewrap and both native host executables on PATH, run the following commands from the repository. The replay assembles a local archive and uses the repository’s plugin components.

```sh
go build -ldflags='-X main.version=v0.0.1-audit' -o /tmp/kitsu-native-audit ./cmd/kitsu
python3 docs/benchmarks/native-host-setup.py /tmp/kitsu-native-audit
```

## How much output reaches the coordinator during verification?

On the earlier build, `task test --brief` printed a short JSON receipt on stdout, but streamed every line from the assertion and the declared checks to stderr. Hosts that capture both streams received the entire transcript. The revised brief receipt points to the retained log of the actual assertion or the first unsuccessful declared check. The gate now records the run it launched directly, which also removes two redundant worktree observations per newly executed gate check. Full text and full JSON verification continue to stream child output.

We replayed the unchanged upstream qs suite and 19 behavior assertions with full TAP output, compact off and zero model calls. The numbers below count stdout and stderr of the verification command. The independent grader read back the retained log, validated its SHA-256 and compared every behavior result across builds and against plain shell.

| Candidate | Plain shell bytes | Earlier Kitsu brief bytes | Revised Kitsu brief bytes | Behavior results | Next action |
| --- | ---: | ---: | ---: | ---: | --- |
| Original | 92,599 | 92,964 | 670 | 2/19 passed | Verify |
| Partial fix | 90,800 | 91,165 | 670 | 18/19 passed | Verify |
| Upstream fix | 90,056 | 179,205 | 581 | 19/19 passed | Done |

All three passed the original suite. Kitsu ran the additional declared repository check after the reference candidate passed the targeted assertion. Plain shell in this control streams its output and can also be redirected to a file. The result measures the bytes delivered by default for this explicit brief request. Reading a diagnostic log adds its selected bytes back to the host context. Wall times in the data are single local observations. The paid qs coordinator stopped before entering verification, so this improvement addresses a separate cost. Its effect on total model usage is unmeasured. [Per-build measurements](daily-review-data/qs-brief-output.json) · [Replay with `--full-suite-output`](daily-verify-qs.py)

## What reached the reviewer on each qs repair?

We replayed the four recorded review reservations five times with fresh task tickets. The control used the previous binary's full task lookup. The revised coordinator forwarded `kitsu task show ID --review-ticket TICKET` and the reviewer opened it with its checkout mounted read-only. Both paths retained the request, exact review candidate and work note, along with all five, three and one prior findings for the three repair reviews. Scripted actors submitted the historical judgments. These measurements describe two ways to transfer the recorded inputs. A coordinator that already has the relevant evidence can also relay it directly.

| Median local payload tokens across four rounds | Previous full lookup | Reserved reviewer input |
| --- | ---: | ---: |
| Coordinator command responses | 1,875 | 1,633 |
| Command forwarded to the reviewer | 68 | 221 |
| Reviewer lookup response | 11,401 | 2,921 |
| All three combined | 13,342 | 4,775 |

The combined payload decreased **64.2%**. Each count uses the local `o200k_base` encoder on the exact command response or forwarded command. Five repetitions account for variations in generated tickets and timestamps. The table takes each median independently. It excludes source reads, model prompts, reasoning, answers and host framing. The coordinator still launches the same four reviewers. The larger forwarded command carries a reservation ticket. Current provider usage and solver wall time remain unmeasured.

All four candidate-specific test suites passed. Running the final submitted tests against the pinned candidates exposed 9, 1, 2 and 0 failed assertions in order. The first two replayed suites terminated early. The final candidate passed all 1,726 assertions. These controls show that the reviewer can reach the correct candidate and its known defects. Whether a model would discover the defects from this input is still open. A released or completed ticket cannot retrieve a new review, and submitting approval against changed source is rejected. [Per-round observations and build identities](daily-review-data/qs-reviewer-input.json) · [Offline replay](daily-review-input-qs.py) · [Reservation regressions](../../scenarios/harness/reviewer_input_test.go)

## Can a task finish with evidence from the wrong files?

Yes, on the previous build. An assertion prepared a CRLF file and passed on those bytes. A later repository check restored LF. Git normalized both versions to the same tree. Both the implementation and verification-only entry paths reported success, although the same assertion failed on the delivered file. Kitsu now checks the raw candidate and its Git parents after each completed verification command. A command that changes the candidate and exits 23 retains that exit status and asks for fresh verification. Previously it dispatched a behavior repair.

An independent review also used to lose its effect if the test log disappeared after commit. With a one-review limit the task paused, although its approved source and commit parents still matched. The revised harness requests the missing verification and reaches done with the existing review once the check passes. An extra commit with the same tree and unexpected parent still needs review. Older verification receipts can be refreshed through the ordinary task command. The checks observe files at command boundaries. Changes reversed within a single command, ignored dependencies and behavior outside the assertions remain outside this evidence. [CLI regression cases](../../scenarios/harness/verification_candidate_test.go)

We replayed three pinned qs candidates five times per build and arm, alternating the build order, for 60 local observations with compact off. The upstream suite passed on every candidate. Both Kitsu builds and the plain shell grader agreed on every individual behavioral outcome.

| Candidate | Behavior assertions passed | Plain shell, revised batch | Previous Kitsu | Revised Kitsu |
| --- | ---: | ---: | ---: | ---: |
| Original | 2/19 | 0.546 s | 0.702 s | 0.714 s |
| Partial fix | 18/19 | 0.540 s | 0.698 s | 0.713 s |
| Upstream fix | 19/19 | 0.541 s | 1.176 s | 1.214 s |

Times are local medians. On the upstream fix Kitsu also ran the declared repository check. Its response remained 477 bytes across builds. The replay measured verification time and correctness. It made zero model calls, so it supplies no new provider-token or end-to-end solver timing data. [Observations and build hashes](daily-review-data/qs-verification-inputs.json) · [Replay driver](daily-verify-qs.py)

## Can a different green suite erase a known failure?

On the pinned partial qs repair, the original suite passed while the public scalar-append case still failed. The previous CLI accepted the suite as a replacement for the failed assertion and returned done on that same defective source. The revised CLI refused the silent substitution. After installing the upstream fix, a single `task test ID --ticket TICKET --retry` reran the saved assertion and reached done on the corrected candidate. Both builds accepted the upstream control. This was a local replay with zero model calls. [Observations and binary hashes](daily-review-data/qs-assertion-retry.json) · [Replay with `--check-failed-retry`](daily-verify-qs.py)

A separate CLI scenario exercises the transition through repair and independent review. Approval of a plausible repair plus a green unrelated suite used to certify a command that still printed the wrong value. The revised harness keeps that failure open. After the actual repair and a fresh review, the scenario retries the saved command, resumes at the commit ticket and verifies the delivered commit. A verifier correcting an invalid command can record a replacement reason, and the task history retains the failed command and run. Whether a changed test implements the developer's intent still requires judgment. [Repair and correction scenarios](../../scenarios/harness/assertion_recovery_test.go)

## Can another coordinator safely finish interrupted work?

Two CLI controls exposed stale reviewer reservations. Restoring an earlier approved candidate while a reviewer still owned a newer patch made the previous harness report done on a review task and issue a commit ticket on a delivery task. The new `reconcile` state shows the outstanding owner, original ticket and review round. Once the host reconciles and releases the worker, the valid original approval can take effect. For hosts with a durable native worker ID, the coordinator can link it to the reservation. Replaying that link consumes no new slot, and a late result from a different ID is rejected. Scripted Codex and Claude reservations exercise the same CLI behavior. The native host remains responsible for launching and cancelling workers. [Recovery regressions](../../scenarios/harness/claim_recovery_test.go) · [Operator guide](../task-harness.md#recover-an-interrupted-reviewer)

We also took the **actual saved ledger and approved Git tree** from the budget-stopped qs run. In isolated copies, Kitsu resumed at the expired deadline, received a new local two-minute window, reused all four review records and ran verification before committing. Plain shell began with the same tree, index and parent and ran the same behavioral and declared checks before committing. We alternated arm order over three pairs. Timing starts after fixture reconstruction and ends at commit. The caller checked the final Git tree, ordered parents and clean checkout separately.

| From approved patch through local commit | Plain shell | Resumed Kitsu |
| --- | ---: | ---: |
| Clean commits on the exact approved tree | 3/3 | 3/3 |
| Behavior checks per run | 19/19 | 19/19 |
| Submitted suite assertions per run | 1,726/1,726 | 1,726/1,726 |
| Median local wall time | 1.053 s | 1.881 s |

All six runs passed the declared repository suite. Kitsu appended only the test and commit events. Its extra time includes reading the saved task, renewing the expired deadline and maintaining evidence. This establishes that the stopped task could be resumed to verified delivery offline. The original paid run remains incomplete. This replay made zero model calls and cannot measure a model's ability to recover unaided or its token cost. [Per-run observations](daily-review-data/qs-resume-delivery.json) · [Reproduction driver](daily-resume-qs.py)

## Does a completed publication still exist?

On the previous CLI, an explicit confirmation returned success after one of two configured push destinations lost the verified branch. The saved ticket bypassed Git. A separate case rejected a matching open PR when its source repository was the second configured push destination. Both reproduced through the normal CLI. The revised confirmation checks the exact ref on every configured destination, then checks the PR against its possible source repositories. Local task views show the last saved outcome, commit, target, PR and observation time without network access. A failed live recheck supplies a recovery ticket and keeps the earlier commit and tests. The PR integration control uses a local Git transport and a scripted GitHub CLI response for an open, closed and reopened PR. [Regression cases](../../scenarios/harness/publication_recheck_test.go)

We extended isolated copies of the completed qs task with a **new, local-only publication request**. Each copy published to two disposable bare repositories. After confirmation, the fixture removed the topic branch on the second destination. Both paths detected the missing branch and confirmed the same verified commit after the fixture branch was republished. The plain path read both destinations directly with Git. The Kitsu path recorded the scope extension, returned a ticket when the live check failed and kept the task's review and verification. Times exclude repository setup, the injected deletion and independent readback.

| Local operation, median of three runs per arm | Plain shell | Kitsu |
| --- | ---: | ---: |
| Publish to both destinations and confirm | 1.519 s | 1.626 s |
| Detect the missing branch, restore and confirm | 0.550 s | 0.642 s |

All six copies retained clean commits, passed the 19 behavior checks and all 1,726 submitted assertions. Neither reviewer nor coordinator model calls were needed. This local replay measures published-ref correctness and CLI time. Live GitHub authentication, network latency and autonomous host recovery remain outside it. [Per-run measurements](daily-review-data/qs-publication-recovery.json) · [Replay with `--local-publication`](daily-resume-qs.py)

## Can a developer install the same harness that was tested?

The public v0.0.1 Linux installer passed its archive checksum and installed successfully. Its CLI returned exit 2 for `kitsu task open --help`. The public release has no matching daily marketplace archive. The install guide now distinguishes that release from the daily preview.

A local GoReleaser v2.18.3 snapshot produced six platform archives, their Syft SBOMs, both installers and a single checksummed `kitsu-plugin.zip`. In a clean, network-isolated Linux environment the native Codex and Claude Code clients registered the extracted local marketplace, discovered both task and gate skills and passed Kitsu’s setup checks. Claude exposed all seven roles and executed SessionStart. The four Codex hooks awaited manual trust. The installed CLI verified an existing change, committed the verified tree, and resumed at `done` with a clean checkout under a scripted driver. Overlaying only the published v0.0.1 task skill in the disposable installations made each host’s setup check exit 5 and name the missing harness protocol. This experiment made zero model requests. This earlier run exercised local Linux setup. A later hosted preview covered all six platforms. Autonomous Claude coding remains open. [Build checksums and native results](daily-review-data/matched-plugin-install.json) · [Install a matching build](../release-install.md#register-the-matching-marketplace)

A fresh Linux profile exposed an installer failure on the routing preview. The bundled command with only its local archive directory looked for `kitsu_v0.0.1_linux_amd64.tar.gz`; the actual bundle contained `kitsu_v0.0.1-routing-preview_linux_amd64.tar.gz`. The updated shell installer selected the correct native tag on that same bundle and installed revision `dcb3a97`. A subsequent Linux/amd64 working build installed the CLI and plugin with a single command on Codex 0.169.0 and Claude Code 2.1.325. Both clients passed local preflight after the download was removed. A separate offline run upgraded an earlier Kitsu marketplace with the same plugin version and repeated the installer on both hosts. Hosted native results for the later preview follow below. [Engineering handoff](kitsu-harness-agent-handoff.md#next-dispatch-from-the-current-preview)

Hosted CI found a defect in the documented Windows download command. Windows PowerShell 5.1 and PowerShell 7 rejected the omitted optional host before installing the CLI. The repaired script validates an explicitly supplied host and accepts a CLI-only install. A separate Codex inventory test now closes the server input and waits for shutdown before removing its temporary profile. The first preview had one cleanup failure. [Original run](https://github.com/belevtsev/kitsu/actions/runs/44279675992)

At revision `5b905fc`, all 16 combinations passed across Linux, macOS and Windows, amd64 and arm64, Codex 0.169.0 and Claude Code 2.1.325, with both PowerShell editions on Windows. Each row installed and reinstalled the matched artifacts, found the task skill using the actual host, ran setup diagnostics and previewed task startup. A fresh CLI process then resumed verification of a staged patch. The runner checked the assertion outcome and preserved HEAD, index and working files. The scenarios made zero model requests. A release and an autonomous Claude coding judgment remain separate decisions. [Per-platform results](daily-review-data/native-platform-matrix.json) · [Artifacts and test logs](https://github.com/belevtsev/kitsu/actions/runs/44281399393)

A subsequent Windows ARM run at `f2e4efb` showed why the installer evidence also checks cleanup. The Codex skill was visible and verification reached its requested finish, but a helper retained a handle to the temporary profile after the inventory server exited. The test now runs the inventory server with Kitsu's existing process supervision and exercises a helper that inherits its streams. Separately, the Windows Go suite exposed a damaged token archive being treated as empty and a CSV test transporting quoted data through Windows command-line parsing. The token reader now diagnoses the damaged path, and the CSV test sends its original field values through stdin. [Run history](https://github.com/belevtsev/kitsu/actions/runs/44283140986)

## Can a resumed coordinator commit to the wrong branch?

Yes in the earlier build. A reviewer approved the candidate and the behavior check passed. After staging that exact tree, switching branches at the same parent still allowed the old ticket to create a commit on the other branch and report `done`. Detaching HEAD had the same result. A commit ticket now includes the Git destination. The CLI exposes it in text, JSON and brief views and checks the destination around the Git commit. The saved proof names the intended destination so an interrupted commit can be reconciled. A clean commit on a different branch leaves delivery unresolved if the intended branch lacks it. When Git already committed on the recorded branch and the checkout moved to another branch still at its parent, the CLI identifies the delivered commit and withholds a duplicate ticket. Returning to the original checkout can reuse current review and verification evidence. [Branch and detached-HEAD regressions](../../scenarios/harness/commit_target_test.go)

We replayed the saved qs task three times per arm with a branch switch immediately after staging. The plain shell driver explicitly compared its intended branch with Git’s current ref. Kitsu rejected the prior ticket and reported the changed target. Both drivers restored the original branch and committed there, leaving the other branch at its original parent. All six runs passed 19 behavior cases, 1,726 submitted assertions and the declared repository suite. Kitsu reused the four recorded reviews. Median elapsed time for the scripted verify-and-deliver sequence was 1.095 s with plain shell and 1.984 s with Kitsu. Both ran without model calls. These times include the injected switch and recovery and describe local process overhead. [Measured runs](daily-review-data/qs-commit-target.json) · [Replay using `--switch-at-commit`](daily-resume-qs.py)

The branch-authority fix also passed two new plain/Kitsu pairs on the archived qs task. Another branch at the same HEAD produced a blocked Kitsu receipt with both refs and no new commit ticket. Returning to the intended branch reused the saved evidence and committed once. Each arm ran the same substantive checks on the restored source. [Per-run recovery results](daily-review-data/qs-scoped-delivery.json)

## What can a coordinator recover in a fresh session?

Previously, `task list` rendered opaque daily task IDs as “saved.” After choosing one, a coordinator could recover its entire ledger with `task show ID --json`. The documented `task next ID --json` handoff returned the request and latest review but could omit earlier acceptance criteria, the accepted plan and the latest repair note. A separate defect let an interactive resume launch a host while a managed interval owned the worktree. Daily mutations were refused only after the host was running.

The list now includes a single-line request excerpt and the last recorded action. The full next-action view includes earlier intake, plan and latest work notes, plus an explicit flag for whether the review applies to the live candidate. The brief transition view keeps its existing scope. During a managed interval, daily inspection names the interval and pending action. No daily ticket or interactive host launch is issued until it finishes. The terminal regression uses an actual managed reviewer reservation and checks successful resume after finishing it. [Recovery tests](../../scenarios/harness/resume_context_test.go) · [Host preflight](../../internal/cli/onboarding_terminal_linux_test.go)

On three copies of the saved qs ledger, the previous preview’s next response was 3,590 bytes and omitted the intake and repair notes. Its full history provided them in 13,189 bytes. The new next response was 4,362 bytes and matched the original request, scope, intake, latest repair, final review and pending verification. The separate plan-and-repair regression checks recovery of the plan and invalidation of the prior approval after an edit. All six verify-and-commit replays reached a clean commit, passed 19 behavior cases and 1,726 submitted assertions, and passed the declared suite. The median verify-and-commit time was 1.051 s for plain shell and 1.855 s for Kitsu. Timings exclude fixture reconstruction and the diagnostic read comparison. No additional reviewers or model requests were used. [Per-run results and build identities](daily-review-data/qs-coordinator-resume.json) · [Replay with `--coordinator-recovery --previous-cli`](daily-resume-qs.py)

## Can a fresh coordinator recover a failed repository check?

In a verify-only CLI task, the requested behavior passed and a declared lint check exited 23. The earlier resume response pointed at the passing assertion, and a lost response after the eventual successful retry made the same retry ticket fail. The revised harness identifies the failed check and its log on resume, retains that failure in task history after recovery, and accepts a replay of the successful ticket while verification still applies. [CLI regression](../../scenarios/harness/repository_retry_test.go)

We replayed the archived, approved qs tree and its saved ledger three times per arm. Both arms received the same injected one-time failure of the declared suite. All six runs executed that suite exactly twice, passed 19 behavior cases and 1,726 submitted assertions, and committed the expected tree with a clean checkout. Kitsu also recovered the failed run and log digest from a fresh CLI invocation, then handled a duplicate successful retry without starting another check. Median time through commit was 1.070 seconds with plain shell and 2.479 seconds with Kitsu. The Kitsu interval includes the extra resume, diagnostic read and duplicate request. These are scripted recovery timings. The replay used zero model calls. [Per-run results and binary identity](daily-review-data/qs-repository-retry.json) · [Replay with `--retry-repository-check`](daily-resume-qs.py)

## Can the implementer finish a design that failed its acceptance check?

The frozen preview returned `verify` after an implementation failed its acceptance example. Running the saved assertion was possible, but the developer could not claim and record a subsequent implementation against that ticket. The revised implementation-only flow returns `implement`, retains the failure and requires a change to record a correction. A transient failure can retry before the correction is claimed. An independent review reproduced a dead end after an empty commit moved the base while the failing source stayed identical. The corrected flow obtains a new verification ticket there. Regression scenarios cover a failed behavior, a failed declared lint check, a transient dependency and a changed base. A verification-only request retains its original scope. [Implementation regressions](../../scenarios/harness/implementation_retry_test.go)

## Can a developer deliver a staged fix without including other edits?

We tested the linked-worktree procedure on a small CSV writer whose staged and working versions deliberately gave opposite answers for embedded quotes. The fixture also contained different staged and unstaged notes, an untracked draft and Git configured to fix trailing whitespace during patch application. The procedure copied the selected entry directly from the saved index tree. We checked the staged entry identity, original branch, index, status and file bytes independently in every attempt. A standard-library CSV reader graded five public cases. [Developer procedure](../task-harness.md#deliver-selected-staged-files) · [Reproduction](staged-delivery.py)

Three pairs per control produced six correct task-only commits and six correctly withheld commits, equally divided between plain shell and Kitsu. Both arms passed 5/5 examples on the good staged patch and 4/5 on the defective patch, and preserved the original checkout in all 12 attempts. Median time from selecting the staged tree through the commit decision was 0.038 s plain and 0.198 s Kitsu on the good patch, and 0.033 s plain and 0.126 s Kitsu on the faulty one. Grading and post-run inspection outside that interval are excluded. These are scripted verification and delivery costs on one fixture, with zero model calls. [Per-attempt observations](daily-review-data/staged-delivery.json)

## Which starting stages and stopping points have offline coverage?

The stage matrix exercises a plan with an open product decision, implementation from an approved design, review of a staged C++ patch, verification of a JavaScript export, repair of a partly fixed Go package and delivery from a saved proof. The plan fixture checks recovery of the submitted plan. The staged review grades the reserved index against an opposing working copy. The JS suite passes on both versions, while the independent behavior check detects a type-contract defect. The repair fixture checks the original failure against an inadequate repair and a corrected implementation. Delivery checks include a scoped local commit, rejection of unrelated staged notes and confirmation against a disposable Git remote. The author and reviewer judgments are scripted. [Stage scenarios](../../scenarios/harness/stage_outcomes_test.go)

A separate CSV journey compares an uninterrupted coordinator with fresh processes at nine handoff boundaries. Each recovers the task from its original request and saved state. Both paths check public output and the committed tree, retain the initial failed run and use two reviewer launches. The interrupted case recovers the pre-link worker from scripted native history and reads the post-link worker ID from Kitsu. A caller with no access to native history still cannot recover a running worker during the gap between launch and registration. [Interruption journey](../../scenarios/harness/interrupted_journey_test.go) · [Worker recovery procedure](../task-harness.md#recover-an-interrupted-reviewer)

A targeted delivery control also interrupted the flow after Git advanced the branch and before the ledger recorded the commit. The frozen CLI recognized the committed tree, but rejected a replay of the original commit ticket while publication was pending. The new CLI records the observed Git hash on the original assertion when it recovers that exact receipt. It preserves the publication ticket and can confirm the disposable remote without repeating verification or creating another commit. [Commit acknowledgement regression](../../scenarios/harness/commit_history_test.go) · [Per-scenario results](daily-review-data/stage-controls.json)

## Local verification

The repository passed `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `golangci-lint run ./...`, and `kitsu change check`. The matched GoReleaser Linux snapshot passed the no-build-tools installer scenario. All six target binaries cross-built. The Go, JavaScript and C++ task journeys use scripted host judgments. Live Codex observations cover a completed pflag pair and the budget-stopped Kitsu arm on qs. Hosted native installer results are recorded above. Release publication and a model-driven Claude Code coding task remain open.

## Does verify-only catch a release regression on another real library?

We compared plain Go and Kitsu on two archived revisions of Masterminds/semver. A caller asked whether allowing a prerelease in one constraint group also admits prereleases in another group joined by `||`. Each revision passed its own upstream suite. The old tests call an internal method whose signature changed, so moving that suite to the fixed revision fails to compile. The shared judge instead made the same four calls through the exported API on both revisions.

| Input | Plain Go, 3 attempts | Daily Kitsu, 3 attempts | Median wall seconds, plain / Kitsu |
| --- | --- | --- | --- |
| Original implementation | Rejected 3/3, three of four cases correct | Kept verification open 3/3 | 0.161 / 0.249 |
| Upstream fix | Accepted 3/3, four of four correct | Finished verification 3/3 | 0.163 / 0.262 |

The original rejected `1.0.1-beta` inside a group that explicitly admitted prereleases. Both candidates correctly excluded the prerelease tested in the other group. The timed plain arm ran the public command. The Kitsu arm opened a verify-only task, recorded intake and ran the same command. After timing, the grader independently checked the actual exit, outputs, saved log digest and unchanged checkout. Each revision's suite and probe were warmed before timing. These 12 local runs demonstrate that the harness carried the verdict and evidence through to the requested finish. No model ran and the probe was supplied to both arms. [Per-run data](daily-review-data/semver-verification.json) · [Reproduction script and pinned inputs](semver-verify-pair.py)

## Will a plausible answer finish the developer’s actual task?

The decision replay starts from a raw request and records the actions of a scripted coordinator. A separate grader checks the resulting source, Git state, task record and command output. Its controls include a correct diagnosis without a recoverable task, an edit staged and restored during a read-only request, a reviewer who inspects an empty working diff, a passing suite that never exercises the reported defect, and a commit made before the stated prerequisite. Direct implementation and a review-led repair are both acceptable when they reach the requested finish and preserve the developer's work. The suite calibrates these judgments. How often an autonomous host chooses the right action is still open. [Requests, controls and replay instructions](harness-request-decisions.md) · [Per-control results](daily-review-data/request-decisions.json)

The delivery controls exposed a distinction that previously required reconstructing the history. `delivery=committed` could mean the current proof ran on an existing commit or that Kitsu observed a commit matching the previously verified working tree. The brief and full receipts now report `commit_evidence`. A current green check documents present behavior. A developer who requested checks before committing can also see which order the evidence supports. [Regression](../../scenarios/harness/commit_history_test.go)

## Will verification stop at the authority the developer granted?

The same faulty CSV export has two correct outcomes. “Verify and commit if it passes” should report the embedded-quote failure and preserve the developer's files. “Finish this repair, review it and commit here” permits a developer worker and a source change. We replayed each route with plain Git/Python and the daily CLI using the same scripted host. Each arm ran six scenarios twice. Both arms produced the intended result in **12/12** attempts. The independent grader accepted an alternative `csv.writer` implementation and rejected six invalid controls, including a duplicate worker launch and committing after checking the wrong file version. On the mixed-index repair, Kitsu named the unrelated staged note and refused the commit. Both HEAD and the index remained intact.

| Median seconds on the faulty patch | Plain Git/Python | Daily Kitsu |
| --- | ---: | ---: |
| Check only, preserve failure and report | 0.044 | 0.140 |
| Repair, recover worker, review, verify and commit | 0.377 | 0.770 |
| Same repair with an unrelated staged edit that prevents commit | 0.377 | 0.756 |

These are local process times with scripted actors and zero model requests. For example, an unauthorized manual edit followed by a refreshed verify ticket, or a green assertion that never checked quotes, could yield plausible Kitsu records. The grader rejected both. A real host still has to choose and respect the developer's scope and test the right behavior. [Portable results and CLI digest](daily-review-data/csv-task-permissions.json) · [Reproduce the request pair](harness-request-decisions.md#verify-only-authority-and-delegated-repair-on-the-same-csv-patch)

Two product defects surfaced during review. A newly requested commit could acquire a fresh ticket after switching to another branch at the same HEAD. A check that switched branches could even produce a completed verification on the new branch. New tasks pin the authorized destination, block on a different branch and withhold verification if a check moves it. The other defect affected terminal output. After a candidate changed, `task next` could print the previous approval or finding as current. It now labels the earlier review, keeps its findings available and points to the saved record. [Branch regressions](../../scenarios/harness/commit_authority_test.go) · [Review output regression](../../scenarios/harness/resume_context_test.go)

## Release and product decisions

An offline cross-check applied the archived review's three exploratory qs cases to the saved plain, managed and daily implementations. Each case ran with both values of `allowSparse` and `throwOnLimitExceeded`. The probe compares the candidate's behavior at the requested array limit with its own behavior at a larger limit on the same sparse input. Library hashes matched the [archived candidate controls](daily-review-data/qs-archived-quality.json).

| Candidate | Scalar before explicit index | Sparse object input | Named and numeric keys mixed |
| --- | ---: | ---: | ---: |
| Original | 0/4 | 0/4 | 2/4 |
| Upstream fix | 0/4 | 0/4 | 2/4 |
| Plain Codex | 4/4 | 4/4 | 2/4 |
| Managed Kitsu | 4/4 | 4/4 | 2/4 |
| Budget-stopped daily Kitsu candidate | 4/4 | 4/4 | 4/4 |

The daily candidate preserved the same result in two more mixed-key configurations. These were exploratory cases selected after reviewing the patch. The upstream fix also disagreed with the probe, and the stopped arm started from a partial repair. Treat this as a concrete example to adjudicate against the developer's contract before making it a blocking finding. All three implementations passed the separate 19-case acceptance grader. For reproduction, use the saved `broader.cjs` from `daily-qs-review-pair-nov18` (SHA-256 `26b3c39c0d35e802b311c319014c660e79f7a427bb93519364ad2a4ad95ae93a`), set its library import to the sandbox's `/work/lib`, and apply it to each archived library with the pinned Node toolchain. This check used zero model calls.

The [engineering handoff](kitsu-harness-agent-handoff.md#assignments-and-current-evidence) tracks the remaining product decisions, their controls and the completed preview installation checks.

Judge daily orchestration by whether the requested work reaches its agreed finish with valid evidence, recoverable handoffs and the developer's changes intact. Count tokens and time for every attempted workflow. A failed or unfinished delivery counts even when its tests pass.

1. **Exercise the whole handoff first.** Cover intake from an existing patch, a review finding, an attempted repair, independent re-review, rerunning the observed failure and verified delivery. Interrupt at each handoff and check that the developer can resume with the correct owner, evidence and next action. The qs replays now cover assertion substitution and resuming the saved paid task to a clean commit offline. Autonomous completion of that handoff within a live model budget remains unverified.
2. **Establish the quality advantage on tasks where it matters.** Keep separate fixtures for planning, reviewing a pre-existing patch with a seeded defect, repairing a partial fix, and verifying a candidate whose existing suite misses the requested behavior. For each, retain a known-good control, a faulty control, expected stopping point and observable outcome. Resolve ambiguous oracle cases with a maintainer. Track missed defects and false alarms separately. Anyone considering a release should be able to reproduce those results offline.
3. **Ship a matched install before promoting daily adoption.** Build a versioned CLI and plugin from the same revision. On native Linux, macOS and Windows, install from the release artifacts in a clean user environment, inspect the host, open a task and resume it. Publish the matching installation commands with the release. All 16 native installer combinations passed for the preview. A public release needs a versioned candidate and an owner decision. Model-driven Claude delivery remains open.
4. **Keep extra model work accountable.** Inspect repeated coordinator calls, recovered claims and reviewer starts alongside the defects they found. Keep compact opt-in. Use offline regressions for routine changes. When a live comparison has separate authorization, reserve a budget for each arm, include every worker and retry, and report correctness, delivery, elapsed time and actual token coverage together. Token savings are an empirical result; dependable coordination is the release priority.
