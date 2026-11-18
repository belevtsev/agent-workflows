# Take a coding task from its current state

Start in the repository that holds your work. Choose the finish you need, such as a reviewed diff, a verified fix or a delivered commit. The task record keeps the current owner, unresolved findings and evidence so another session can continue. Install the matching CLI and plugin, then check your host. [Installation guide](release-install.md#register-the-matching-marketplace)

```sh
kitsu agent check --host codex
kitsu task start "Review the staged export change, repair confirmed findings, and verify CSV quoting" --until verify --host codex
```

For Claude Code use `--host claude`. From an open session, ask Codex to use the Kitsu task skill or invoke `/kitsu:task` in Claude. The host inspects the issue, design or diff and records the requested outcome before it edits. For a review only, say “review this diff” and select `--until review`. For a design, select `--until plan`. The interactive host setup and any permission prompts still apply. Use `--print` to preview the handoff.

| Starting point | What to expect |
| --- | --- |
| Idea or requirement | Plan with acceptance examples, trade-offs and open questions. |
| Approved design | Implement, add coverage and verify the requested behavior. |
| Diagnosis only | Ask “why does this fail?” and select `--until plan`. The result can be a confirmed failure with a reproduction, or a specific gap in the evidence. Source and index stay intact. |
| Bug or partial fix to finish | Establish expected and actual behavior, inspect the supplied fix, repair confirmed defects and retry the public assertion. |
| Existing diff or PR checkout | Independent review with findings tied to the inspected content. A completed review can have open findings. |
| Candidate ready for testing | Run an actual behavior assertion, then prioritized and remaining repository checks. |
| Delivery | Report readiness. A local commit requires explicit scope and a verified, fully staged candidate. Explicit publication scope keeps delivery open until the remote branch and requested PR are confirmed. |

For an implementation-only request, a failed behavior assertion or declared check returns the task to `implement`. Read the failure receipt, correct the implementation, record the new result and retry the saved assertion. For a transient failure, the unchanged candidate can retry before a correction is claimed. A verify-only request keeps its own editing boundary.

For a conditional request such as “check my patch and commit it here only if it passes,” use a verify entry and a delivery finish with commit authorization. If the behavior fails, report the failing command and stop with the patch, staging and branch intact. `task next` can still offer a verification retry. A new request to repair the patch can extend its scope.

### Review a committed change

A clean working tree can contain a whole PR. To review a specific pair of revisions, open a review-only task with both ends of the comparison, then start your usual host on the saved task.

```sh
kitsu task open review-export --entry review --until review --review-base origin/main --review-head HEAD --request "Review the committed export change for incorrect CSV quoting"
kitsu task start --resume review-export --host codex
```

Choose the base you intend to compare. For a branch that diverged from main, use the merge base as the starting revision when you want the branch's changes since divergence. The reviewer receives the resolved commit IDs and commands to inspect their log and diff. Another session can recover that comparison after a branch moves while the original Git objects are available locally. Open a new task to review updated endpoints. For an ordinary staged or working-tree review, omit the two revision options.

## Inspect or resume

```sh
kitsu task list
kitsu task next export-quoting
kitsu task start --resume export-quoting --host codex
kitsu task show export-quoting --json
kitsu usage show --task export-quoting --json
```

Find a daily task by its request excerpt and last recorded action in `kitsu task list`. The list reads saved records. Use `kitsu task next ID --json` to inspect the live candidate, original request and scope. The latest result carries its note. Earlier intake criteria, the accepted plan and the latest implementation or repair note appear under `prior_notes` when applicable. These notes describe what the participants reported. In the terminal, `task next` labels findings from an earlier candidate and points to its saved review. In JSON, check `review_current` before relying on an earlier review. The full history, including superseded repairs, remains in `task show ID --json`.

If a managed worker interval is active in the same worktree, an unfinished daily task reports `blocked`, identifies the interval and preserves the pending daily action. Finish the managed interval, then resume. Interactive startup checks this before launching the host.

From a repository, interactive daily startup pins the working tree and prints the task ID before opening the host. The host binds the saved request to its scope. A launch failure leaves the ID resumable. Previewing the handoff creates no task. From an already open host, the task skill chooses a short ID at intake. The next-action view shows the latest result, claim, assertion and delivery state. The full view retains the request and earlier results. These records live in the worktree's private Git directory. Switch back to that worktree to resume. Existing shared OpenSpec changes still use the [managed workflow](workflow.md).

When reporting a delivered commit, read `commit_evidence` in the receipt. `verified_existing_commit` means the commit was already checked out when the current proof ran. `commit_matches_verified_tree` means Kitsu subsequently observed the committed tree on the verified parents. If the request required running checks before committing, report the order the evidence actually shows.

## Drive the harness without opening a host

Open a task with an explicit starting activity and finish line. The example authorizes repairs and stops after verification.

```sh
kitsu task open export-quoting --entry review --until verify --repair --request "Review and fix CSV quoting"
kitsu task next export-quoting --json
```

Each action returns a ticket for its successor. Pass that ticket to the next command. Record the actual host session as the actor. Notes can hold a short artifact or point to a design, reproduction, source location or run log.

1. To record intake and reserve the reviewer in one call, append `--claim-review codex --brief` to the intake command below. Use `claude` for Claude Code. Start the worker only on a `claim_created` receipt. A replay returns the current claim for recovery. The same option works on implementation and repair results. Record intake with `kitsu task record export-quoting intake --actor SESSION --ticket TICKET --note "Expected embedded quotes to round-trip"`.
2. If the review is still unclaimed, reserve it with `kitsu task claim export-quoting --actor SESSION --ticket TICKET --host codex --brief` before starting the worker. For Claude Code use `--host claude`. Read `claim.reviewer.model` for the model and effort, then load the original skills listed in `claim.reviewer.skills` and check their SHA-256 hashes. A missing or disabled selected skill leaves the reservation open to retry. The reservation and subsequent review retain the requested settings. For a subsequent review, the brief receipt includes a `review_changes` command showing the change since review, including newly admitted work when a staged review expands. A changed staged tree in review-only mode has its own `review_index_changes` command. The receipt flags a changed base or scope. Pass the returned `review_input` command to the new reviewer in the same worktree. It reads the reserved candidate, developer request, intake, plan, latest implementation or repair note, previous review findings, and chosen reviewer settings from the task record. It works with a read-only checkout. Reported test results in notes still need checking. The review return validates the current source, index and base against the ticket. Older reservations can still use `task show ID --json`. When the review base or scope changed, inspect the full candidate. Inspect the diff and repository skills. Record the result with `task record ... review` and the reviewer's session. Supply one `--finding` per unresolved issue. If the process stops after reservation, look up the original worker. Release its ticket only after confirming that worker is finished or cancelled.
3. Follow the returned action. For an authorized repair, add a focused failing test, correct the implementation and record the result. An independent reviewer checks the changed candidate again.
4. At `verify`, execute a command that asserts the agreed outcome. For example, use `kitsu task test export-quoting --brief --ticket TICKET --expect "Embedded quotes round-trip" -- go test ./csv -run TestQuotedField`. Commands run in the repository with their normal local permissions. With `--brief`, command output goes to retained logs. Read `verification_log.path` to diagnose the executed assertion or the first unsuccessful repository check. Omit `--brief` to see output live. Kitsu checks the Git tree, original file bytes, HEAD and merge parents for each command. If an assertion or repository check changes those inputs, the receipt reports `source_changed` and verification stays open. After restoring the intended candidate, retry with the current ticket. Older daily verification receipts require a fresh run to establish this evidence. If a completed task loses its test log, Kitsu can keep its independent review when the delivered bytes, Git tree and ordered parents still match. Test output and logs are evidence for the assertions they actually make. A passing command that tests the wrong thing still needs human or reviewer correction.
5. Inspect the readiness decision. A missing executable, failed assertion, changed candidate or missing retained proof keeps verification open. A repository without declared checks shows `undeclared` alongside its task assertion. To continue an approved plan later, run `kitsu task extend ID --until verify`.

### Delegate implementation or repair

When you ask for a developer agent to do the coding, the coordinator reserves the current implementation or repair action with the host’s selected developer model, reasoning effort and skills.

```sh
kitsu task claim export-quoting --actor COORDINATOR --ticket TICKET --host codex --brief
kitsu task claim export-quoting --actor COORDINATOR --ticket TICKET --worker WORKER --brief
kitsu task record export-quoting implement --actor WORKER --ticket TICKET --note "CSV quoting regression added and fixed" --brief
```

Use `repair` for a repair result. The host starts the worker between the first two commands only when the first receipt has `claim_created`. It uses the assignment in `claim.developer`, including the launch label. A fresh coordinator recovers the linked worker from `task next`. If a launch was interrupted before linking, search the native host history by that label before releasing the reservation. Release a reservation only after accounting for its worker. The worker’s return must name the linked native session. A linked worker may return completed work after its deadline on the assigned base. The task remains paused before starting any further work. Independent review still needs a fresh reviewer when the task calls for one. The direct path remains available when one session can do the coding.

### Retry verification

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

Only request `--commit` with `--until deliver` when a commit is in scope. After verification, inspect and stage the task-owned files. Use the commit ticket from verification directly. Use `kitsu task commit ID --ticket TICKET --message TEXT`. Each pre-existing edit included in the commit needs an explicit `--include PATH`. A partial index cannot certify a commit whose tree differs from the verified candidate. Leave unrelated work intact and deliver the verified working change when a separate commit would mix ownership. After interruption, `task next` checks the committed tree and its ordered parents. This includes the extra parents of a pending merge. A merge still needs a commit when its file contents already match HEAD. If a Git hook changes the committed tree or moves the checkout during `git commit`, Git may already have written a commit when Kitsu reports the mismatch. Inspect both branch tips and the created commit, then resume with `task next` before attempting another commit.

Use `--time-limit 10m` at intake to pause actions after a deadline. Verification commands inherit the deadline. Hosts own their native workers and their cancellation. After reconciling any outstanding claim, `task extend ID --time-limit 5m` grants a new window. JSON reports observed sessions, repairs, elapsed time and time to the first result. Captured tokens sum available task-attributed host notices. A reviewer-only total can omit the interactive coordinator. A null value means usage is unavailable. Missing host notices prevent a complete cost comparison.

To bound a review loop, pass these options to `kitsu task start`, `task open` or `task extend`. For example, use `--review-limit 3 --time-limit 12m --finish-reserve 2m`. Interactive startup pins the limits before opening the host. These example values allow three review reservations over the task lifetime and stop **new** reviews in the final two minutes. Released reservations count. A bounded reservation returns `review_return_by` for the host to pass to its worker. A running reviewer can return its original, current result, including after the deadline. The hard deadline still applies to verification and delivery. The reserve controls launches and leaves running worker cancellation to the host. These controls measure starts and time. Actual token consumption depends on the host.

When the next required review cannot start, `task next ID --brief` returns `paused`, the reason, the pending action, the candidate and a command to resume. Follow `details` for outstanding findings. Approval and verification requirements still apply. With an authorized additional slot, use `kitsu task extend ID --review-limit 4` to set a new total. Use `--time-limit` to renew the time window.

A new commit-scoped task pins its destination branch when opened or extended. Interactive startup saves the starting checkout before launching the host. When resuming, use `kitsu task next ID --brief` to see `commit_target`, such as `refs/heads/feature` or `HEAD` for a detached checkout. On another branch the task can report `blocked` with `current_commit_target` and no ticket, even when both branches point to the same commit. Return to the recorded checkout to reuse applicable evidence. If a check changed branches, rerun it there. For an intentionally different delivery destination, start a newly authorized task. If the original branch already received the verified commit, Kitsu checks its tree and parents before acknowledging it. An older task can recover its destination from its saved proof. A record that never captured a branch cannot establish an earlier destination.

If a commit response was lost, check `task next`. When it already reports committed, replaying the original commit ticket can acknowledge that Git result even while publication is pending. If Git advanced before Kitsu saved its commit event, the recovered commit hash appears as `observed_commit` on the original test event in `task show ID --json`. The publication ticket remains usable.

For a requested push, pin `--publish-to origin/topic` when opening or extending a delivery task. For a GitHub PR also pin `--pr-base main`. The host performs the authorized push or PR creation and checks for existing work after an interruption. At the publish action, run `kitsu task confirm ID --ticket TICKET`, adding `--pr https://github.com/OWNER/REPO/pull/NUMBER` for a PR. This command reads the configured push destination and checks the advertised branch hash. For a PR it also queries the installed authenticated GitHub CLI for the open PR URL, head branch and commit, and requested base. When a remote has several push destinations, every destination must advertise the verified commit. A PR can originate from any of their matching repositories.

After delivery, `kitsu task next ID --brief` reads the saved `last_publication` receipt locally. It includes the remote and branch, commit, observation time and PR URL when requested. To check the remotes and saved PR live again, run `kitsu task confirm ID --brief`. If a branch moved or disappeared, the PR closed, or a destination could not be reached, the recheck reports the error. The failure receipt supplies the next action and ticket. The task keeps its local verification and commit. Inspect the destination before deciding whether any push is appropriate. After connectivity or publication is restored, use the new ticket to confirm, adding `--pr URL` for a requested PR.

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

Publication confirmation checks every configured push destination. For a GitHub PR, Kitsu compares the head repository owner and name, branch, commit and requested base. The JSON field `token_coverage` describes the available attributed notices. A partial total is a lower bound.
