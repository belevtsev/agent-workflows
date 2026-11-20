# Take a coding task from its current state

Open the Git worktree that holds your change in Codex or Claude Code. Choose what you want back from this session. For example, if you have a staged CSV export change, ask Codex:

> Use Kitsu’s task skill to review my staged export change. Repair any confirmed CSV quoting bugs, verify the behavior, and stop before committing. Give me the task ID and check results.

In Claude Code, start the same request with `/kitsu:task`. For a design or diagnosis, ask for a plan. For a review only, ask for findings and say to stop after review. [Install and check the plugin](release-install.md#check-the-host-and-start-a-task)

You can also launch a host from your terminal:

```sh
kitsu task start "Review the staged export change, repair confirmed findings, and verify CSV quoting" --until verify --host codex
```

With a [trusted runner](workflow-runner.md), use `kitsu task run "Fix CSV quoting" --until verify` to run the supervised workflow. It uses version 2 of `kitsu-roles.toml` or the saved default host when the repository has no roles file. [Configure role assignments and resume a run](configured-workflow.md)

## How a daily task runs

<img src="diagrams/daily-task.svg" alt="The developer asks the host agent for work. The task skill opens a local record. Kitsu returns the next action, the host performs it, and Kitsu records the result. A new chat resumes from the same record until the requested outcome is reached." width="100%">

Codex or Claude Code loads the task skill and coordinates the work. Kitsu's CLI saves the request, scope, findings and check results under this worktree's Git directory. Ask a fresh chat in the same worktree to resume your task ID. The agent reads `kitsu task next ID --brief` to recover the current action or return an already-finished result. If you lost the ID, ask it to list your saved tasks.

For example, a plan for a CSV export can finish with acceptance examples. To verify a repair, the agent supplies an executable assertion such as exporting a field containing a comma and reading the value back. Kitsu runs the assertion and any declared repository checks and records which candidate passed. A local commit needs your authorization and a matching staged tree. A push or PR requires publication scope. [Independent review](#choose-independent-review) · [Managed specialist workflow](workflow.md#the-flow)

| Starting point | What to expect |
| --- | --- |
| Idea or requirement | Plan with acceptance examples, trade-offs and open questions. |
| Approved design | Select `--until implement` to implement, add coverage and verify the requested behavior in the working tree. |
| Diagnosis only | Ask “why does this fail?” and select `--until plan`. The result can be a confirmed failure with a reproduction, or a specific gap in the evidence. Source and index stay intact. |
| Bug or partial fix to finish | Establish expected and actual behavior, inspect the supplied fix, repair confirmed defects and retry the public assertion. |
| Existing diff or PR checkout | Independent review with findings tied to the inspected content. A completed review can have open findings. |
| Candidate ready for testing | Run an actual behavior assertion, then prioritized and remaining repository checks. |
| Delivery | Report readiness. A local commit requires explicit scope and a verified, fully staged candidate. Explicit publication scope keeps delivery open until the remote branch and requested PR are confirmed. |

A plan-only task keeps its opening working tree, staged tree and Git parents as the inputs for intake and planning. Intake can record new observations. If those inputs change before the plan is recorded, the plan action reports `blocked`. Inspect the changes and keep any developer work. To plan against newly intended inputs, open a new task. The existing task resumes if its opening inputs match again. A completed plan remains available after later edits. Kitsu observes snapshots at commands, so a temporary edit restored between commands can go undetected.

When opening a task directly with `kitsu task open`, supply `--until` and give the original `--request` unless startup already saved it. Use `--entry` to choose a different starting activity. For example, `--entry implement --until deliver --commit` authorizes implementation through a local commit. Run `kitsu task open --help` to see all choices.

For an implementation-only request, a failed behavior assertion or declared check returns the task to `implement`. Read the failure receipt, correct the implementation, record the new result and retry the saved assertion. For a transient failure, the unchanged candidate can retry before a correction is claimed. A verify-only request keeps its own editing boundary. For an uncommitted patch, a passing check can report `verified working tree` while an independent review is pending or has findings. `ready to commit` means the next action has a commit ticket and the staged tree matches the verified tree. Existing edits still need the requested path inclusions.

For a conditional request such as “check my patch and commit it here only if it passes,” use a verify entry and a delivery finish with commit authorization. If the behavior fails, report the failing command and stop with the patch, staging and branch intact. `task next` can still offer a verification retry. A new request to repair the patch can extend its scope.

### Review a committed change

A clean working tree can contain a whole PR. To review a specific pair of revisions, open a review-only task with both ends of the comparison, then start your usual host on the saved task.

```sh
kitsu task open review-export --entry review --until review --review-base origin/main --review-head HEAD --request "Review the committed export change for incorrect CSV quoting"
kitsu task start --resume review-export --host codex
```

Choose the base you intend to compare. For a branch that diverged from main, use the merge base as the starting revision when you want the branch's changes since divergence. The reviewer receives the resolved commit IDs and commands to inspect their log and diff. Another session can recover that comparison after a branch moves while the original Git objects are available locally. Open a new task to review updated endpoints. For an ordinary staged or working-tree review, omit the two revision options. The first reviewer receives a `review_diff` command for the reserved working tree. When the index holds different content on a review-only task, `review_staged_diff` shows exactly what was staged. Use the comparison the developer requested. Both commands continue to address the reserved objects after the checkout changes.

## Deliver a bug ticket

From the target repository, open a task from a public link, a Markdown file, or a tracker connected through MCP or a plugin. The command authorizes a local repair and commit on the current branch. Ticket text supplies the problem statement. Publishing a branch, posting to the tracker and closing the issue require separate developer action.

<img src="diagrams/ticket-delivery.svg" alt="Kitsu saves the ticket source, records a failing reproduction, then reruns the same assertion after repair and on the verified local commit. A passing final run yields a source-linked receipt. A failing run leaves the log for inspection." width="100%">

Follow one assertion through the diagram. It fails on the original bug, passes on the staged repair, then passes again on the delivered commit. Those three observations link the reported behavior to a particular Git commit.

```sh
kitsu task bug wrapper-args --from https://tracker.example.org/tickets/wrapper-args.md
kitsu task start --resume wrapper-args --host codex
```

For a tracker exposed through MCP or a host plugin, ask Codex or Claude Code to read the ticket with your configured server. For example, “Read EXP-14 from our support tracker and use Kitsu to reproduce, repair and verify it.” The host passes the returned title and description as Markdown on standard input. Use the ticket's stable browser link, or an `mcp://SERVER/TICKET-ID` or `plugin://PLUGIN/TICKET-ID` reference when the server only returns an ID. A file exported by another tool works the same way:

```sh
kitsu task bug export-14 --from mcp://support/EXP-14 --stdin < ticket.md
```

The host handles tracker access. Kitsu records the supplied text and source reference. Use a permanent link without credentials or temporary access tokens. Resume offline with `kitsu task next export-14`.

For a direct HTTP(S) link, Kitsu makes an anonymous connection to a public IP address. Each redirect gets the same destination check, and direct imports ignore HTTP proxy settings. The reader accepts UTF-8 Markdown, plain text, or server-rendered HTML with readable content in a main or article element. Review the imported snapshot at intake. For an internal, authenticated, proxy-only, or client-rendered ticket, let your configured host connector supply the Markdown with `--stdin`. You can keep the private URL as the source reference in that command.

Opening any supported ticket prints the path to a private Markdown snapshot in the worktree's Git directory. JSON exposes it as `ticket_markdown`. It includes the canonical source link and saved request. Repeat the same open command to regenerate it offline. The delivery receipt always links to the original source.

For a local ticket use `--from ./tickets/wrapper-args.md`. Use `--host claude` for Claude Code. A Markdown import needs only a readable UTF-8 file. Kitsu saves the text and source digest once. Reopening the same task and source reuses the saved version, even offline. Choose a new task ID to use a revised ticket. `task next ID` shows the saved request, source and next action. Add `--require-review` when opening the task for an independent review. Repository review requirements still apply.

1. Record intake with the observable bug. Run the reproduction through `task test ID --ticket TICKET --expect "expected behavior" -- PROGRAM ARGS`. If you add a reproducer to the checkout, refresh the verification ticket with `task next ID` before running it. Choose a command whose nonzero exit demonstrates the particular defect. Inspect its output. A missing program or a green first check leaves reproduction open.
2. Repair the code, preserve existing edits, add regression coverage and stage the intended files. Record the changed candidate with `task record ID repair --actor SESSION --ticket TICKET --note "what changed"`. Follow independent review if required.
3. At verification, use `task test ID --ticket TICKET --retry`. Kitsu reruns the same reproduction and the repository checks. At the commit action run `task commit ID --ticket TICKET --message "Fix wrapper argument handling"`. Use `--include PATH` to authorize a relevant edit that already existed at intake.
4. Run `kitsu task receipt ID --json` with the delivered commit checked out. Kitsu executes the saved behavior assertion again, checks the working files, index, commit and target, and saves the receipt on success. A failed run prints the path to its log. Repeating the command after success retrieves the historical receipt, including after the branch advances.

The receipt contains the source URL (a file URI for Markdown), title and digest, Git target, commit and tree, expected behavior, argument vector and log references for all three observations. Repository check references identify evidence from verification before commit. An undeclared check suite is labeled `undeclared`. Use the receipt as the delivery summary when reporting back to the ticket.

## Choose independent review

For new daily tasks, “implement, test and commit” uses one coordinator by default. If a reviewer finds a defect in the implementation or a behavior test fails after review, the developer can correct the work under the original implementation scope. For a review finish, Kitsu records the findings and stops. Reviewing or verifying a supplied patch requires explicit repair scope before editing it. When the correction changes the candidate, required review and the saved assertion run again before delivery. An explicit review entry or review finish reserves an independent reviewer. To require that same review during implementation, use a plain-language request, pass `--require-review` to `task start` or `task open`, or add it later with `task extend ID --require-review`. A review limit caps starts and does not request one. Use an explicit `--entry implement` when opening an implementation task directly. With only `--until deliver`, a new task starts by verifying the existing candidate.

For a required review, the plugin records `SubagentStart` and `SubagentStop` from a separate native reviewer under the reserving Codex or Claude Code session. The worker's final JSON refers to the reserved review and candidate. The coordinator records it with `kitsu task record ID review --ticket TICKET --observed-review --brief`. The `host_observed` result records what the local host reported. `agent_reported` describes an optional CLI result supplied directly. If the hooks are missing or the worker returns an unmatched result, required review stays pending. Verify the [plugin hook setup](hooks.md#verify-plugin-hook-setup) and recover the reserved worker before retrying.

To require review for the repository, put the following top-level key before any TOML tables in `kitsu.toml`.

```toml
version = 1
require_review = true
```

The CLI checks the declaration in the committed and working snapshots at intake and at the current checkout. Removing the policy during a task cannot waive the original requirement. An invalid declaration leaves the review policy unknown. The task can record implementation work, then blocks with a diagnostic before verification. Correct the declaration or explicitly request review if the saved baseline is invalid. Executable repository checks still need a valid current declaration. A plan-only task can still finish with a plan. When verification is the starting stage, Kitsu checks the supplied behavior first. Required review must still finish before delivery. Tasks saved by earlier CLI versions retain their original review routing. The managed workflow keeps its own role and approval rules.

## Inspect or resume

```sh
kitsu task list
kitsu task next export-quoting
kitsu task start --resume export-quoting --host codex
kitsu task show export-quoting --json
kitsu usage show --task export-quoting --json
```

Find a daily task by its request excerpt and last recorded action in `kitsu task list`. The list reads saved records. Both hosts resume with `kitsu task next ID`. This shows the saved request and scope, prior work, findings, verification and next action. An active reservation also shows how to recover its worker. The terminal and brief views skip token accounting. Use `kitsu task next ID --json` when you need exact candidate hashes or captured usage. Open `task show ID --json` when you need an earlier event or a fact the snapshot omits. If `usage_error` appears, the accounting archive needs attention. The task result and ticket still describe the action, and `captured_tokens: null` means the total is unknown. The latest result carries its note. Earlier intake criteria, the accepted plan and the latest implementation or repair note appear under `prior_notes` when applicable. These notes describe what the participants reported. In the terminal, `task next` labels findings from an earlier candidate and points to its saved review. In JSON, check `review_current` before relying on an earlier review. The full history, including superseded repairs, remains in `task show ID --json`.

If a daily task record is unreadable, `kitsu task list` still shows healthy tasks and exits with status 1. Use `kitsu task next ID` on a healthy row to continue. In a partial JSON result, `list_error` explains why the inventory is incomplete and `daily_error` identifies an affected daily record. Listing leaves the saved records intact.

If a managed worker interval is active in the same worktree, an unfinished daily task reports `blocked`, identifies the interval and preserves the pending daily action. Finish the managed interval, then resume. Interactive startup checks this before launching the host.

From a repository, interactive daily startup pins the working tree and prints the task ID before opening the host. A newly launched host receives the scope-binding command with the saved ID and requested finish. On an interrupted task, the host reads the current state before acting. A launch failure leaves the ID resumable. Previewing the handoff creates no task. If you pass limits with `--print` or `--json`, the prompt includes the limit flags for `task open`. An interactive launch saves those limits before the host opens. Its printed resume command includes `--compact` when you selected it. From an already open host, the task skill chooses a short ID at intake. The next-action view shows the latest result, claim, assertion and delivery state. The full view retains the request and earlier results. These records live in the worktree's private Git directory. Run `git worktree list` to locate the original checkout, enter its working directory, then run `kitsu task list` to confirm the ID. Both `kitsu agent check` and interactive startup report a Git metadata directory or an unreadable Git configuration before opening the host. Existing shared OpenSpec changes still use the [managed workflow](workflow.md).

### Share a verification receipt

```sh
kitsu task receipt export-quoting --markdown > verification.md
```

Paste the receipt into a PR or ticket after verifying a working patch or commit. It identifies the saved tree, exact commit when known, verification order, repository checks, review and ticket. Later checkout changes leave that observation intact. Recognized GitHub issue links are canonical. For other sources, the task name, source type and content digest identify the saved ticket. Full source references, raw logs, arguments, notes, action tickets, session IDs and local paths stay in the local record.

The declaration fingerprint identifies the bytes pinned at intake. The review row distinguishes a saved host-observed review from an agent report and applies only to the verified candidate. Older records may have a verified tree with no exact commit ID or declaration pin.

Markdown reads saved evidence without running checks. For a ticket-linked bug, use `kitsu task receipt ID --json` on the delivered commit to run the final behavior check and save the complete local receipt. A later Markdown export includes that result.

Use `commit_evidence` in `kitsu task next ID --json` for the programmatic verification order. `verified_existing_commit` means the commit was already checked out when the proof ran. `commit_matches_verified_tree` means Kitsu later observed the matching tree on the verified parents.

## Drive the harness without opening a host

Open a task with an explicit starting activity and finish line. The example authorizes repairs and stops after verification.

```sh
kitsu task open export-quoting --entry review --until verify --repair --request "Review and fix CSV quoting"
kitsu task next export-quoting --json
```

Each action returns a ticket for its successor. Pass that ticket to the next command. Record the actual host session as the actor. Notes can hold a short artifact or point to a design, reproduction, source location or run log.

1. To record intake and reserve the reviewer in one call, append `--claim-review codex --brief` to the intake command below. Use `claude` for Claude Code. Start the worker only on a `claim_created` receipt. A replay returns the current claim for recovery. The same option works on implementation and repair results. Record intake with `kitsu task record export-quoting intake --actor SESSION --ticket TICKET --note "Expected embedded quotes to round-trip"`.
2. If the review is still unclaimed, reserve it with `kitsu task claim export-quoting --actor SESSION --ticket TICKET --host codex --brief` before starting the worker. For Claude Code use `--host claude`. Read `claim.reviewer.model` for the model and effort, then load the original skills listed in `claim.reviewer.skills` and check their SHA-256 hashes. A missing or disabled selected skill leaves the reservation open to retry. The reservation and subsequent review retain the requested settings. For a subsequent review, the brief receipt includes a `review_changes` command showing the change since review, including newly admitted work when a staged review expands. A changed staged tree in review-only mode has its own `review_index_changes` command. The receipt flags a changed base or scope. Pass the returned `review_input` command to the new reviewer in the same worktree. It reads the reserved candidate, developer request, intake, plan, latest implementation or repair note, previous review findings, and chosen reviewer settings from the task record. It works with a read-only checkout. Reported test results in notes still need checking. The review return validates the current source, index and base against the ticket. Older reservations can still use `task show ID --json`. When the review base or scope changed, inspect the full candidate. Inspect the diff and repository skills. Record the result with `task record ... review` and the reviewer's session. Supply one `--finding` per unresolved issue. If the process stops after reservation, look up the original worker. Release its ticket only after confirming that worker is finished or cancelled.
3. Follow the returned action. For an authorized repair, add a focused failing test, correct the implementation and record the result. When independent review is required, the reviewer checks the changed candidate again.
4. At `verify`, execute a command that asserts the agreed outcome. For example, use `kitsu task test export-quoting --brief --ticket TICKET --expect "Embedded quotes round-trip" -- go test ./csv -run TestQuotedField`. Commands run in the repository with their normal local permissions. With `--brief`, command output goes to retained logs. Read `verification_log.path` to diagnose the executed assertion or the first unsuccessful repository check. Omit `--brief` to see output live. Kitsu checks the Git tree, original file bytes, HEAD and merge parents for each command. If an assertion or repository check changes those inputs, the receipt reports `source_changed` and verification stays open. After restoring the intended candidate, retry with the current ticket. Older daily verification receipts require a fresh run to establish this evidence. If a completed task loses its test log, Kitsu can keep its independent review when the delivered bytes, Git tree and ordered parents still match. Test output and logs are evidence for the assertions they actually make. A passing command that tests the wrong thing still needs human or reviewer correction.
5. Inspect the readiness decision. A missing executable, failed assertion, changed candidate or missing retained proof keeps verification open. A repository without declared checks shows `undeclared` alongside its task assertion. To continue an approved plan later, run `kitsu task extend ID --until verify`.

Keep the ticket from the start of implementation or repair while editing and staging files. If HEAD or the pending merge parents change, inspect the new base with `kitsu task next ID` before recording work. After a CLI upgrade, refresh an unclaimed coding ticket issued by an older build. A linked developer can still return using its original reservation on the recorded base.

### Delegate implementation or repair

When you ask for a developer agent to do the coding, the coordinator reserves the current implementation or repair action with the host’s selected developer model, reasoning effort and skills.

```sh
kitsu task claim export-quoting --actor COORDINATOR --ticket TICKET --host codex --brief
kitsu task claim export-quoting --actor COORDINATOR --ticket TICKET --worker WORKER --brief
kitsu task record export-quoting implement --actor WORKER --ticket TICKET --note "CSV quoting regression added and fixed" --brief
```

Use `repair` for a repair result. The host starts the worker between the first two commands only when the first receipt has `claim_created`. It uses the assignment in `claim.developer`, including the launch label. A fresh coordinator recovers the linked worker from `task next`. If a launch was interrupted before linking, search the native host history by that label before releasing the reservation. Release a reservation only after accounting for its worker. The worker’s return must name the linked native session. A linked worker may return completed work after its deadline on the assigned base. The task remains paused before starting any further work. Independent review still needs a fresh reviewer when the task calls for one. The direct path remains available when one session can do the coding.

### Retry verification

The task pins the exact `kitsu.toml` from its opening commit, or its opening tree before the first commit. The next-action view identifies that policy as `repository_policy`. If the candidate changes or removes it, restore the opening declaration to continue. To adopt a reviewed policy, open a new task from a commit that includes it.

For a verify-only request such as “commit if it passes,” a failed assertion or declared check permits a retry on the same worktree, index and Git parents. Changing that candidate requires the developer to authorize repair with `kitsu task extend ID --repair`.

A verification receipt identifies the staged tree as well as the working files and commit parents. If someone stages or unstages a file, use the verification ticket from `kitsu task next ID` and run `kitsu task test ID --ticket TICKET --retry` to check the new index. The saved command may itself inspect staged contents. Kitsu checks the index before and after each assertion and repository check. A change observed during a command leaves the task unverified.

For commit-scoped work, stage the authorized files before running verification to run the checks once. If you stage the entire verified working tree afterward, the original commit ticket can run the saved checks against the new index and proceed when they pass. A failed assertion or declared check keeps the branch at its original commit. A partially staged tree still requires inspection.

A check can exit successfully and still change the files it tested. When you see `source_changed`, the terminal and the JSON field `verification_log.detail` give a short explanation from the runner. Open the retained log for the full detail. In a new Python repository, for example, importing a module can create an untracked `__pycache__/`. Inspect the paths first. If they are generated artifacts, configure the project to ignore them or direct the output outside the checkout. If a tracked source file changed, inspect and resolve that edit. Rerun verification on the intended files before committing.

If you lose the response from an unsuccessful check, run `kitsu task next ID --brief`. Its `verification_log` gives the check ID, run ID, outcome, exit status when available, log path and SHA-256. Use the returned ticket to retry a transient failure on the same candidate. A repair ticket can retry verification on that candidate until someone claims the repair. After a source repair and any required review, run the saved assertion with the new verification ticket.

```sh
kitsu task test export-quoting --ticket TICKET --retry --brief
```

Kitsu reuses a passing assertion and repository checks on an unchanged candidate while their receipts still match. After a newer result supersedes a task's repository failure, the original receipt appears in the assertion event’s `prior_repository_failures` in `kitsu task show ID --json`. Use its run ID to find the retained log at `kitsu/logs/<run_id>.log` inside the common Git directory.

Repeating a successful retry with the same ticket returns the current result without running checks again while the verification remains valid. If a later check supersedes that evidence or a log is missing, run `kitsu task next ID --brief` for the current verification ticket.

A different suite can pass while the original defect still reproduces. Kitsu retains the failing assertion across retries and interruptions. For a mistaken test, supply the corrected command with `--expect` and `--replace-reason "why the original command was wrong"`. The test event in `kitsu task show ID --json` retains the reason, original command, expected behavior and failed run ID. If the corrected command cannot run, the original failure stays open.

### Recover an interrupted reviewer

The review reservation holds its owner, ticket and round. If the host returns a native worker ID you want to recover later, save it on the existing reservation. Use the same ID as the actor when recording that worker’s review.

```sh
kitsu task claim export-quoting --actor COORDINATOR --ticket TICKET --worker WORKER_ID --brief
kitsu task next export-quoting --brief
```

Linking the ID twice is safe. The receipt shows `claim.worker` and the review round, and linking takes no additional review slot. A reservation with a linked worker accepts review results only from that ID. The host supplies the worker identity. For an unlinked reservation, look up the worker in the original coordinator’s native history by its task ID and review-round label. A replacement coordinator can then repeat the link using the saved `claim.owner` for `--actor` and the recovered native ID for `--worker`. When the native history is unavailable, the CLI alone cannot identify an unlinked running worker. Keep the reservation until its status can be reconciled.

If `next` is `reconcile`, Kitsu still has an outstanding claim for another candidate or action. Recover that worker in its host. Its result can be recorded if the reserved candidate and action become current again. After confirming the worker has finished or has been cancelled, run `kitsu task release export-quoting --ticket ORIGINAL_TICKET --brief`. The returned next action reflects the current files and surviving evidence. Restoring an already approved candidate can reuse its review.

## Delivery and limits

Only request `--commit` with `--until deliver` when a commit is in scope. After verification, inspect and stage the task-owned files. Use the verification ticket with `kitsu task commit ID --ticket TICKET --message TEXT`. Each pre-existing edit needs an explicit `--include PATH`. The staged tree must match the verified tree. Leave unrelated work intact and deliver the verified working change when a separate commit would mix ownership.

After interruption, `task next` checks the exact commit tree and ordered parents. A pending merge needs a commit even when its files match HEAD. If a hook changes the committed tree, Kitsu reports the commit it observed and requires fresh verification of that commit. Kitsu reconciles the real index only while it still matches the original index under Git's index lock. If your staging or checkout moved, inspect Git and return to the recorded candidate before resuming. Open a new task for further edits.

After verified delivery, `task next ID --brief` returns the historical `completed_delivery` with the commit, tree and verification outcome. Older records may identify only the committed tree. Later checkout changes leave the saved delivery complete. Start a new task to review, verify or commit later work.

Use `--time-limit 10m` at intake to pause actions after a deadline. Verification commands inherit the deadline. Hosts own their native workers and their cancellation. After reconciling any outstanding claim, `task extend ID --time-limit 5m` grants a new window. JSON reports observed sessions, repairs, elapsed time and time to the first result. Captured tokens sum available task-attributed host notices. A reviewer-only total can omit the interactive coordinator. A null value means usage is unavailable. Missing host notices prevent a complete cost comparison.

To bound a review loop, pass these options to `kitsu task start`, `task open` or `task extend`. For example, use `--review-limit 3 --time-limit 12m --finish-reserve 2m`. Interactive startup pins the limits before opening the host. These example values allow three review reservations over the task lifetime and stop **new** reviews in the final two minutes. Released reservations count. A bounded reservation returns `review_return_by` for the host to pass to its worker. A running reviewer can return its original, current result, including after the deadline. The hard deadline still applies to verification and delivery. The reserve controls launches and leaves running worker cancellation to the host. These controls measure starts and time. Actual token consumption depends on the host.

When the next required review cannot start, `task next ID --brief` returns `paused`, the reason, the pending action, the candidate and a command to resume. Follow `details` for outstanding findings. Approval and verification requirements still apply. With an authorized additional slot, use `kitsu task extend ID --review-limit 4` to set a new total. Use `--time-limit` to renew the time window.

A new commit-scoped task pins its destination branch when opened or extended. Interactive startup saves the starting checkout before launching the host. When resuming, use `kitsu task next ID --brief` to see `commit_target`, such as `refs/heads/feature` or `HEAD` for a detached checkout. On another branch the task can report `blocked` with `current_commit_target` and no ticket, even when both branches point to the same commit. Return to the recorded checkout to reuse applicable evidence. If a check changed branches, rerun it there. For an intentionally different delivery destination, start a newly authorized task. If the original branch already received the verified commit, Kitsu checks its tree and parents before acknowledging it. An older task can recover its destination from its saved proof. A record that never captured a branch cannot establish an earlier destination.

If a commit response was lost, check `task next`. When it already reports committed, replaying the original commit ticket can acknowledge the Git result even while publication is pending. `task show ID --json` identifies the recovered commit in the completed delivery or the original test event. Pending publication is tied to that commit.

For a requested push, pin `--publish-to origin/topic` when opening or extending a delivery task. For a GitHub PR also pin `--pr-base main`. The host performs the authorized push or PR creation and checks for existing work after an interruption. At the publish action, run `kitsu task confirm ID --ticket TICKET`, adding `--pr https://github.com/OWNER/REPO/pull/NUMBER` for a PR. This command reads the configured push destination and checks the advertised branch hash. For a PR it also queries the installed authenticated GitHub CLI for the open PR URL, head branch and commit, and requested base. When a remote has several push destinations, every destination must advertise the verified commit. A PR can originate from any of their matching repositories.

After delivery, `kitsu task next ID --brief` reads the saved `last_publication` observation locally. It includes the remote and branch, commit, observation time and PR URL when requested. Run `kitsu task confirm ID --brief` to check those destinations for the saved commit. A moved branch, closed PR or unavailable remote records an `unconfirmed` observation and returns an error. The original delivery remains complete and requires no ticket to check again.

### Deliver selected staged files

When the developer wants a commit containing selected staged files and also has unrelated changes, start the task in a linked worktree before verification. This example copies the staged version of `export.py` onto a new branch, even when the working copy of that file differs. Inspect the staged diff first, and choose the paths and destination branch the developer requested. The commands assume a repository with an existing commit and a new branch and directory.

```sh
base=$(git rev-parse HEAD)
staged=$(git write-tree)
git diff "$base" "$staged" -- export.py
git worktree add -b deliver-csv ../deliver-csv "$base"
git -C ../deliver-csv restore --source="$staged" --staged --worktree -- export.py
git -C ../deliver-csv diff --cached --exit-code "$staged" -- export.py
git -C ../deliver-csv diff --cached --name-status
```

This procedure requires a resolved original index. The selected-path comparison must exit zero and print no diff. Also check that the staged path list contains exactly the files you selected. For a rename include both paths. Start the task in `../deliver-csv`, verify the behavior there, and include each selected pre-existing path when recording the authorized commit. The original branch, index, unstaged versions and untracked files stay available in the original checkout. Resume the new task from the delivery worktree. Its history starts there.

For the offline developer journeys run `go test -count=1 -v ./scenarios/harness`. The language examples use Go, Node.js or Bun, and a C++ compiler. The hosts are scripted in these tests. Live host validation should use a disposable checkout, run a read-only review with a real independent reviewer, then resume an authorized repair and check the CLI evidence and native session usage on each host.

For routine commands, pass `--brief` to receive a small JSON result with the next action, ticket, any active claim, finding count and verification references. Follow the returned `details` command to read the full request, findings and history after interruption. A review-only result is tied to the index, HEAD and working source. Staging a different patch reopens that review. Extending a staged-only review to delivery triggers a fresh review if the staged tree excluded any working edits. Changing the base commit also reopens the review.

Publication confirmation checks every configured push destination. For a GitHub PR, Kitsu compares the head repository owner and name, branch, commit and requested base. The JSON field `token_coverage` describes the available attributed notices. A partial total is a lower bound. For a solo task, `kitsu usage show --task ID` can also show the whole session measured by the host when the session ID matches an actor recorded on the task. The JSON field `actor_session_usage` holds those receipts. A session can contain several requests, so the task total stays unknown without task-specific evidence.
