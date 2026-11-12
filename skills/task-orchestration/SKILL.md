---
name: task-orchestration
description: Coordinate substantial work with bounded workers; dispatch independent test engineers, reviewers at every stage and routine GitHub workers.
---

# Task orchestration

Plugin root: `${CLAUDE_PLUGIN_ROOT}` (empty outside the Claude Code plugin; see the
[host mapping](references/hosts.md#worker-agent-types) for paths and agent types).

The coordinator owns the outcome, consultation, dispatch, integration and final
verification. Use this skill when a task is substantial: two or more of changes
in more than one package or component, behaviour that must match across
platforms or shells, more than about 300 changed lines, or a design choice with
real alternatives that the source cannot settle. Everything else stays
lightweight: the coder writes the tests, the reviewer checks each against the
Code Review Rules, and commits and pushes run directly under the user's
authorization. Dispatch an independent [test engineer](references/contracts/test-engineer.md)
when a substantial task adds or materially changes behavioural tests, helpers,
fixtures, golden data, fuzz seeds or tolerances, or when the user asks. Publish
a verified substantial task with `cw publish`, drafting the commit message and
PR text yourself; you carry the user's authorization and the command is only the
mechanism. Dispatch the GitHub worker for the fallback cases that
[publication](references/github-worker.md) lists or when the user asks for a
bounded worker. Repository instructions and user authorization still govern.

## Follow the decision flow

Five states in order; no required state is skipped, and earlier evidence never
satisfies a later state.

1. **Classify.** If `cw` is not on `PATH`, stop and give the install command:
   `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.2` with
   Go 1.27.1 or newer, or the release archive
   ([get `cw`](../../docs/operations.md#get-cw)).
   Run `cw doctor --summary --repo /absolute/path` at the start
   and after material workspace changes; resolve profiles from the
   [host mapping](references/hosts.md). `cw task start` records the plan and
   binds the configured advisers. Checkpoints are required for an open decision
   (real alternatives with consequential uncertainty), for any change to code
   that must behave alike on several platforms or shells, and whenever in doubt;
   otherwise the plan declares `"checkpoints": {"required": false, "reason": "..."}`
   and states 2 and 4 are not required
   ([details](references/global-workflow.md#start-and-bind-a-task)). The plan
   cannot change after start; checks can only be added, by a merge-bound
   `cw task add-checks` after a base-branch merge, never removed or narrowed
   ([merging the base branch](references/global-workflow.md#merging-the-base-branch)).
   A task that later meets an open decision stops and asks the user, and never
   restarts over source the first record changed. Decide here whether the plan
   opts in to the
   [adversarial tester](references/contracts/adversarial-tester.md#opt-in-and-record). Hand a
   consequential planning, architecture or diagnosis question that source does
   not settle to a `decision`-profile worker.
2. **Before checkpoint** (when required). With enough source evidence to assess
   approach, alternatives, risks and verification plan, consult with the `before`
   context, adjudicate and attach a resolved record before implementation edits
   or worker dispatch; without implementation, before substantive drafting or review.
3. **Implement and verify.** Build new behaviour along the
   [flow](references/contracts/README.md#flow), with a review loop at every
   stage: spec writer ⇄ reviewer (spec) → architect ⇄ reviewer (design) →
   implementer ∥ test engineer ⇄ reviewer (tests) → integrate and run →
   reviewer (code) ⇄ implementer, with an opt-in
   [adversarial tester](references/contracts/adversarial-tester.md) beside the
   code review for concurrency, parsing or untrusted input, security or data
   integrity, or when the user asks. Findings go back to that stage's author; the
   [review cap](references/worker-handoffs.md#review-stages) applies per stage,
   and the round budget in the same section bounds spec, design, contract and
   test-engineer rounds across the whole task (implementer fixes never count);
   that file says when a small task skips the spec and design stages.
   After state 2, advisers join only at the four
   [adviser points](references/consultation-helper.md#adviser-points-in-the-flow)
   and only when available; without them the flow runs unchanged. Assign and
   dispatch workers, integrate, run the selected checks and obtain the
   independent code review.
4. **Fresh final checkpoint** (when required). Consult with a new request from
   the completed work, current diff, check results and remaining limitations,
   using the `final` context; adjudicate and attach it.
5. **Complete.** Claim completion or publish only after `cw task verify` passes.

An unresolved decision blocks the next state: investigate and adjudicate again,
or consult again when the evidence changes.

## Author questions

Write Choice-only requests under the [question patterns](references/question-patterns.md).

## Adjudicate

Check every judgment against source under the
[adjudication rules](references/consultation-helper.md#adjudicate-without-network-access).

## Keep the coordinator lean

Every request re-sends the whole conversation. These rules never decide whether
a task is substantial or replace a gate.

- Read a reference when its step starts, and only the section needed.
- The `cw task` commands print a one-line summary by default; pass `--summary`
  to `cw doctor` and `cw task trace`. Read a missing value with
  `cw task show --field <dotted.path>`, not the whole record.
- Send long build and test output to a file and print only the exit status,
  result lines and failures.
- Batch each step: `assign` then dispatch in one turn, `handoff && accept` in
  one shell call, embed-and-consult and adjudicate-and-attach in one call each.
- Delegate investigations over many files or long logs to fresh bounded workers
  returning conclusions and exact references; past about 200K tokens, hand
  remaining independent pieces to fresh workers.
- Rerun a passed check only after the source, inputs or environment change, or
  to resolve a recorded risk. Commit after `cw task verify` and compare trees
  instead of rerunning ([why](references/global-workflow.md#commit-after-verification)).
- In a non-interactive session (`claude -p`, `codex exec`) run workers in the
  foreground and wait; a background worker dies with the coordinator.

## Read when the step starts

| Step | Reference |
| --- | --- |
| Host, profiles, policy, paths | [hosts](references/hosts.md) |
| Preset for the kind of work | [use-cases](references/use-cases.md) |
| Scope, plan, `cw task`, checks, completion | [global-workflow](references/global-workflow.md) |
| Advisers, checkpoints, adjudication | [consultation-helper](references/consultation-helper.md) |
| Assign, dispatch, accept, review | [worker-handoffs](references/worker-handoffs.md) |
| Roles, flow, review stages | [contracts](references/contracts/README.md) |
| Independent tests | [test-engineer](references/contracts/test-engineer.md) |
| Commit, push, PR | [publication](references/github-worker.md) |
| Jira, Confluence, GitHub evidence | [connected-sources](references/connected-sources.md) |
| Adviser providers | [adviser-providers](references/adviser-providers.md) |
