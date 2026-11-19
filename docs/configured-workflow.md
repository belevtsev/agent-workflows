# Run a configured role workflow

Use `kitsu task run` from a Git worktree to run a daily task with the repository’s role assignments. A repository without a roles file uses the saved install host for all required roles. Start a task or resume its saved ID from either host.

```sh
kitsu task run "Fix CSV export when a field contains a comma" --until verify
kitsu task run --resume TASK_ID
```

Kitsu reads the saved request and chooses the next action from the task's evidence. The configured host executes each specialist assignment in a fresh session. Install and sign in to each host you configure, then check the plugin and hooks with `kitsu agent check --host codex` or `kitsu agent check --host claude`. Validate the installed models and native hook delivery on a disposable checkout before promoting the workflow. [Host setup](release-install.md#check-the-host-and-start-a-task)

## Start from a chat

In Codex, select the Kitsu task skill and ask “Use Kitsu’s configured workflow to fix CSV quoting and verify the result.” In Claude Code, enter `/kitsu:task Use the configured workflow to fix CSV quoting and verify it.`

To resume, use the Kitsu skill in the original worktree and ask “Resume configured Kitsu task `TASK_ID`.” The saved settings determine each role’s host. When a host needs a terminal launch, run the resume command Kitsu displays.

## Configure the roles

In your repository, run `kitsu role init` to create version 2 of `kitsu-roles.toml` from the install choice. Supply `--host codex` or `--host claude` to seed from a different host. An existing version 1 or 2 file requires an intentional edit.

```sh
kitsu role init
kitsu role set architect --host codex --model gpt-6.1-sol --reasoning max
kitsu role set developer --host claude --model sonnet --reasoning high
kitsu role set reviewer --host codex
kitsu role list
```

You can ask either plugin to make the same changes and display the result. Use repository-scoped commands before starting the task. The settings saved with an active daily run are used on resume.

To edit by hand, use one `[roles.<role>]` table per specialist below. The optional `git` role is for managed commits. Omitting `model` uses the role’s host preset. Omitting `effort` uses the host’s reasoning default. Omit `skills` to inherit personal skill selections, or use `skills = []` for an empty selection. `kitsu role set ROLE --reasoning ''` restores the host’s reasoning default without changing the model.

```toml
version = 2

[roles.lookup]
host = "claude"
model = "haiku"
effort = "medium"

[roles.spec-writer]
host = "codex"
model = "gpt-6.1-sol"
effort = "high"

[roles.architect]
host = "codex"
model = "gpt-6.1-sol"
effort = "max"

[roles.quality-engineer]
host = "claude"
model = "sonnet"
effort = "high"

[roles.developer]
host = "claude"
model = "sonnet"
effort = "high"

[roles.reviewer]
host = "codex"
model = "gpt-6.1-sol"
effort = "max"

[roles.git]
host = "claude"
model = "haiku"
effort = "low"
```

Daily workflows derive test paths from the repository and can use an optional `[paths].tests` declaration as an upper bound. For a managed task, define patterns that match your repository’s test and fixture files. Kitsu opens separate sessions for architecture, writing, and review even when they share a host. It resolves selected skills from their original sources and records the assigned host, model, and effort when the task starts. Run `kitsu role list` to see the configuration. To change an assignment on the next task, edit the file or use `kitsu role set`. [Version 1 settings](roles.md)

## Follow the evidence

The diagram uses the assignments from the TOML example above. Kitsu saves each completed assignment and hands the task to the role configured for the next step.

<img src="diagrams/configured-workflow.svg" alt="Kitsu pins the role settings and dispatches architecture to Codex. It records the Codex plan and sends test writing to Claude Code. Kitsu checks the baseline, Claude Code implements the change, and Kitsu verifies it. A separate Codex reviewer inspects the verified candidate when required." width="100%">

1. The architect reads the current checkout and records the intended behavior, source and test boundaries, and expected baseline.
2. The test writer works inside the approved test paths. Kitsu runs the focused check and retains the observation. For a change in behavior, the pre-code failure must show the planned reason. For a refactor, the pre-code check should pass.
3. The developer works within the plan. Kitsu checks production changes against the accepted tests, then runs the saved assertion and declared repository checks on the resulting candidate. The check command, outcome, and retained log identify what Kitsu observed.
4. When the request or repository requires review, a fresh native reviewer inspects the verified candidate. Review findings route back to the relevant role. Further edits require verification and any required review on the updated candidate.
5. Kitsu reports the requested outcome. With `--until deliver --commit`, it can make an authorized local commit against the verified tree and original destination. Existing edits require explicit inclusions.

Use `--until plan` for an architecture and acceptance plan, `--entry tests --until verify` for test-only edits, or `--entry review --until review` for findings on an existing change. Test-only uses a passing check and skips implementation. Verification of a supplied patch can start at `--entry verify --expect "observable behavior" -- PROGRAM ARGS`. Add `--repair` only when changes to that supplied patch are in scope.

Kitsu runs assertions that match a trusted repository check or a command you supply with `--expect TEXT -- PROGRAM ARGS`. If the test writer proposes different arguments, run `kitsu task run --resume TASK_ID --approve-assertion` in your terminal to review and approve the exact command. The task resumes from the saved test assignment.

If a task is interrupted, enter the original worktree and resume the saved ID. Use `kitsu task show TASK_ID --json` for the full record. Kitsu keeps an outstanding worker reservation until its return is recorded or reconciled. A reported blocker includes the available recovery information. Read the saved evidence before retrying. [Daily recovery and scope](task-harness.md#inspect-or-resume)

## Run a managed OpenSpec role

Ask the plugin to use the managed workflow when you need the shared specification, design, test and review records. The coordinator can use configured host assignments for its existing preparation steps and explicitly run a prepared dispatch:

```sh
kitsu task prepare export-quoting --role quality-engineer --stage tests \
  --round 1 --workflow full --assignment /path/to/test-assignment.md --dispatch
kitsu task dispatch DISPATCH_ID --run
kitsu task return DISPATCH_ID --request HAND_BACK_PATH
kitsu task finish DISPATCH_ID --expect-checks fail
```

Pass the path to an assignment file outside the checkout. Use the dispatch receipt’s `result_path` for `HAND_BACK_PATH`. The test writer’s handback contains its claims. Inspect the executed check and its log before accepting a failure as the pre-code baseline. Use `--expect-checks pass` for a refactor or test-only work.

Configured reviews use a fresh native reviewer under the reserved host parent. Kitsu checks the matching `SubagentStart` and `SubagentStop` notices and the returned candidate. Full OpenSpec stages still need their recorded human approvals and review dispositions. When Claude Code disallows another headless Claude process inside its session, run the saved dispatch from a terminal. Other same-host assignments can use the plugin's native worker.

Repeated `task dispatch DISPATCH_ID --run` can display a saved return without another worker launch. If a launch is unresolved, inspect the indicated host session. After the worker has stopped, use the existing `task finish DISPATCH_ID --abandon --reason TEXT` recovery and prepare a new interval if needed.

If `git` is configured, prepare and run that role with `--stage commit` when the full task shows commit readiness. The worker inspects the candidate read-only. Review the handback, stage only the authorized files, and run the current checks. Then request the local commit with `task finish DISPATCH_ID --expect-checks pass --message "Fix CSV quoting" --include PATH`. Repeat `--include` for every changed path, including changed OpenSpec files and skill receipts that belong in the commit. Kitsu requires the staged tree to equal the checked candidate, an installed gate, and explicit coverage of all changed paths. If you omitted `git`, the coordinator can still report readiness and the developer can make an authorized scoped commit through the gate. [Managed roles and approvals](workflow.md#the-flow)
