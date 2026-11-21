# Run a configured role workflow

Kitsu coordinates Codex and Claude Code from one saved task. For supervised execution, an administrator registers a disposable runner for this worktree. The runner starts each assigned specialist, verifies the accepted files in isolation, and signs the result. Kitsu imports authorized edits, records evidence, and resumes the next step.

[Install the opt-in `kitsu-team` plugin](release-install.md#add-role-coordination) and [set up a trusted runner](workflow-runner.md) before using `task run`. Use the [daily task skill](task-harness.md) for ordinary in-chat work.

## Start and resume from either host

**Codex** · “Use Kitsu’s configured workflow to fix CSV quoting and verify the exported value.”

**Claude Code** · `/kitsu-team:managed Use the configured workflow to fix CSV quoting and verify the exported value.`

Resume in the original worktree. In Codex, ask “Resume configured Kitsu task `TASK_ID`.” In Claude Code, enter `/kitsu-team:managed Resume configured Kitsu task TASK_ID`. Both use the same saved assignments and runner.

```sh
kitsu task run "Fix CSV quoting" --until verify
kitsu task run --resume TASK_ID
```

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

To edit by hand, use one `[roles.<role>]` table per specialist below. The optional `git` role is available for manual handoffs. Kitsu creates an explicitly requested local commit after supervised verification. Omitting `model` uses the role’s host preset. Omitting `effort` uses the host’s reasoning default. Omit `skills` to inherit personal skill selections, or use `skills = []` for an empty selection. `kitsu role set ROLE --reasoning ''` restores the host’s reasoning default without changing the model.

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


The roles file’s optional test paths must match the reviewed runner policy. Use the repository’s own test paths and approved programs.

## Verify or deliver an existing change

Supply the reviewed assertion when opening at verification. The intake assertion stays required during repairs. A test writer can add a separate reviewed assertion. The runner also runs every required repository check.

```sh
kitsu task run "Verify CSV quoting" --id csv --entry verify --until verify \
  --expect "a comma within a field is quoted" -- ./scripts/check-csv
```

Add `--repair` when you authorize source changes. Use `--until deliver --commit` for a local commit and repeat `--include PATH` to authorize pre-existing edits. A configured commit requires standard Kitsu Git hooks or no local commit hooks. Kitsu verifies the exact staged bytes before committing. Add `--require-review` when you need an independent reviewer.

Kitsu applies the configured rework limit, records test and code corrections, and verifies again when assertions or content change. Inspect progress with `kitsu task next TASK_ID --json`.

## Recover a stopped run

Resume the same task after an interruption. Kitsu asks the runner for the existing attempt and consumes its retained result. Resolve a reported blocker before resuming with `--retry`. Use `--release ATTEMPT` only after the runner operator authorizes release and the runner confirms the whole job has stopped.

Completed records from the earlier preview remain historical. Start a supervised task with the original request and intake assertion. Reconcile outstanding legacy workers with the operator before reusing their checkout.
