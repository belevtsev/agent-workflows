---
name: task-orchestration
description: "Runs a substantial code change as a recorded `cw` task: independent tests, review at every stage, then `cw task verify` before the commit. Use when a change spans several packages or components, must behave alike across platforms or shells, runs to several hundred lines, or has a design choice the code cannot settle, so that it meets the substantial-task rule of the working conventions; or when the user asks for the staged flow. Not for small edits, questions or a review alone."
---

# Task orchestration

Plugin root: `${CLAUDE_PLUGIN_ROOT}` (empty outside the Claude Code plugin).

The coordinator owns the outcome, consultation, dispatch, integration and final
verification. Use this skill for a
[substantial task](../../global/AGENTS.md#substantial-tasks). Repository
instructions and user authorization still govern.

## Follow the decision flow

Five states in order; no required state is skipped, and earlier evidence never
satisfies a later state.

1. **Classify.**
   1. If `cw` is not on `PATH`, stop and give the install command:
      `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.2`
      with Go 1.27.1 or newer, or the release archive
      ([get `cw`](../../docs/operations.md#get-cw)).
   2. Run `cw doctor --summary --repo <abs path>`; rerun after a material
      workspace change. Fix what `problems` lists before starting. Resolve
      profiles from the [host mapping](references/hosts.md).
   3. Write the plan and run `cw task start`, which records it
      ([details](references/global-workflow.md#start-and-bind-a-task)). The plan
      is fixed after start; only `cw task add-checks` after a base-branch merge
      adds checks, and none is removed or narrowed
      ([merging the base branch](references/base-merge.md)).
   4. Set `checkpoints.required` to `true` for an open decision, for a change
      to code that must behave alike on several platforms or shells, or when
      unsure. Otherwise declare
      `"checkpoints": {"required": false, "reason": "..."}`; states 2 and 4 are
      then skipped.
   5. Decide whether the plan opts in to the
      [adversarial tester](references/dispatching-reviews-and-tests.md#adversarial).
   6. Hand a consequential planning, architecture or diagnosis question that
      source does not settle to a `decision`-profile worker.
   7. If an open decision, or a change to code that must behave alike on
      several platforms or shells, appears in a task started without
      checkpoints, stop and ask the user. Never start a second task over source
      the first record changed.
2. **Before checkpoint** (when required). With enough source evidence to assess
   approach, alternatives, risks and verification plan, consult with the `before`
   context, adjudicate and attach a resolved record before implementation edits
   or worker dispatch ([checkpoint helper](references/consultation-helper.md)).
   Only when `cw task show --field advisers` is not empty, read the
   [adviser guidance](references/adviser-providers.md), which also applies in
   states 3 and 4 and when checkpoints are not required.
3. **Implement and verify.** Run the [flow](references/contracts/README.md#flow)
   of the task's profile: `lite` by default; `standard` for an open design
   choice or a public interface change; `strict` when the user asks or the
   change touches security, data integrity or concurrency.
4. **Fresh final checkpoint** (when required). Consult with a new request from
   the completed work, current diff, check results and remaining limitations,
   using the `final` context; adjudicate and attach it as in state 2.
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
- Start each stage with a new worker from the output of
  `cw task brief --task <task-id> --stage <stage>`, never a continued worker.
  The worker loads only the one contract the brief names.
- Read a worker's report file only to adjudicate a finding.
- Read a missing value with `cw task show --field <dotted.path>`, not the
  whole record.
- Send long build and test output to a file and print only the exit status,
  result lines and failures.
- Delegate investigations over many files or long logs to fresh bounded workers
  returning conclusions and exact references.
- Past about 150K tokens, hand over and stop: record the results in flight,
  then give the user the task ID, the output of `cw task brief` for each
  remaining stage and any decision the record does not hold. A fresh
  coordinator continues from those.
- In a non-interactive session (`claude -p`, `codex exec`) run workers in the
  foreground and wait; a background worker dies with the coordinator.

## Read when the step starts

Each step has one file. A worker's contract path comes from its brief; the
roles file lists the contracts.

| Step | Reference |
| --- | --- |
| Terms and role names | [glossary](references/glossary.md) |
| Host, profiles, policy | [hosts](references/hosts.md) |
| Scope, plan, `cw task`, checks, completion | [global-workflow](references/global-workflow.md) |
| Checkpoints, adjudication | [consultation-helper](references/consultation-helper.md) |
| Assign, dispatch, accept, review | [worker-handoffs](references/worker-handoffs.md) |
| Roles, flow, stage contracts | [contracts](references/contracts/README.md) |
| Independent tests, code-review findings, adversarial tester | [dispatching-reviews-and-tests](references/dispatching-reviews-and-tests.md) |
| Choice-only questions | [question-patterns](references/question-patterns.md) |
| Commit, push, PR | [publication](references/github-worker.md) |
| Jira, Confluence, GitHub evidence | [connected-sources](references/connected-sources.md) |
| Cost and outcome metrics, when asked | [measure-outcomes](references/measure-outcomes.md) |
