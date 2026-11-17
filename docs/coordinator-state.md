# Coordinator state snapshots and deltas

Use one bounded task view after a meaningful event such as a worker return,
check run, finding disposition or policy change. It runs locally in Go and
starts no agent, check or adviser. Compact output is explicit; normal
`task show` and `task show --json` retain their existing behavior.

```sh
kitsu task show fix-login --compact
kitsu task show fix-login --since <snapshot-id>
kitsu task show --help
```

The first response has `version:1`, `kind:"snapshot"`, `snapshot_id`, `task`,
`worktree`, `created_at`, `scope` and all eight `sections`. Save the complete
view before using its id as a baseline. The response defaults to **16 KiB**;
`--max-bytes` accepts 512 through 1048576 bytes, including metadata and newline.
If complete sections do not fit, the command fails with no partial JSON and
saves no new baseline. Increase the budget; nothing is silently truncated.

| Section | Facts and references |
| --- | --- |
| `source` | Git tree and HEAD, source visibility, raw write-content and task-record keys. Raw keys detect ignored records and changes hidden by Git clean filters. |
| `task` | Full task's first unfinished recorded stage, content ids, broken-rule and nonautomated-scenario counts; command reference for details. Small histories keep `stage:"unknown"`. |
| `interval` | Active worktree interval, role, stage, round, host, original packet path/hash, expiry and policy changes since preparation. An interval for another task is labeled `other_task`. `status:"none"` explicitly clears it. |
| `checks` | Current declared-check verdicts, run ids, warnings and evidence key. A `run_id` names `<log_directory>/<run_id>.log`; expand that original log or use the gate-status command reference. No complete run records or logs are repeated. |
| `findings` | Every open finding in canonical full-task `reviews.md`, with id, stage, severity, original path/line and content key. Finding prose stays at the original reference. Small-task review completeness is `unknown`. |
| `policy` | Keys for check declarations, effective role/model/reasoning policy, selected original skills for both hosts, adviser settings/binding and source visibility. Private configuration bodies and credentials are not returned. Unavailable skill selections are explicit problem ids. |
| `prerequisites` | Known record blockers and next-stage requirements, open findings, adviser checkpoints, active interval, unavailable skill policy, missing current passes, uncommitted content and enforcing-hook prerequisites. Details have expansion references. This is not a complete semantic workflow audit. |
| `git` | Index identity, whether it matches the worktree, whether the worktree matches HEAD, gate mode, hook state and a reference to ordinary gate status. |

The active interval's optional `return` contains a private return-file reference,
finding/severity and blocker counts, a claims key and whether its recorded
observations still match current source/evidence/policy. Claims stay labeled
`untrusted-worker-claims`. Inspect the referenced return after a worker stops;
do not forward author prose to an independent reviewer. Closed interval returns
are not a canonical finding-disposition history. Record findings in the normal
task record and inspect primary sources. The view does not validate role writes;
`task finish` retains that responsibility.

## Apply a delta

`--since` implies compact JSON. A delta has `kind:"delta"`, `since` naming the
base, a new `snapshot_id` and only the changed sections. Kitsu independently
reads current state; it does not replay or trust worker answers as check evidence.

1. Require matching version, task, worktree and the exact saved `since` id.
2. Replace each returned section in the saved view as a whole. Keep absent
   sections unchanged. Empty arrays and explicit `status:"none"` clear old facts;
   `null` removes a section if a future compatible projection requires removal.
3. Retain the new id after applying the entire delta. A subsequent delta may
   reference that id: Kitsu stores a complete baseline behind every successful
   response, even when the response itself is a delta.
4. Expand changed references when needed for a decision. Never use an isolated
   delta to reconstruct state after losing the base; request `--compact` again.

An unchanged view returns `sections:{}`. A changed section is indivisible, so
its unchanged fields may reappear. A check completing can change `checks` and
`prerequisites` without any source edit; evidence expiry and damaged records also
change the live verdict. Policy changes remain visible even without an active
interval. The interval's `policy_changes` compares against its prepared policy;
changed policy keys in a delta compare against your saved snapshot. Abandon and
reprepare when the active interval's policy has changed or expired.

## Baseline lifetime and failure handling

Snapshots are private, atomic, content-bound JSON under the current worktree's
Git directory, in `kitsu-stages/snapshot-<id>.json`. They share the interval lock
and pinned-directory safety checks. The view creates no tracked records and
never closes or mutates an interval. Snapshot retention is limited to **64**
entries, at most **64 MiB**, and **24 hours**. Eviction removes only snapshots,
never worker packets, returns or finish receipts. Do not copy baselines between
tasks, worktrees or machines.

Unknown, evicted, expired, future-dated, tampered, linked, incomplete or
wrong-task/worktree/version baselines fail explicitly. Recover with a fresh
`task show <task> --compact`. Concurrent state changes or an occupied interval
lock also fail; retry after the relevant operation completes. Original record
links and unsafe directories are refused. Hashes detect accidental substitution;
they do not attest against someone with equivalent filesystem access.
Future-date checks retain the existing local protocol's one-minute clock tolerance.

The collector compares complete observations, including current check verdicts,
effective policy and selected external skill hashes, and checks raw/task source
identity at the observation boundaries. This is a point-in-time local view;
changes after it returns still require refresh and normal final checks.

## Quality and measurement limits

`prerequisites.completion` is always **`not_certified`**. A recorded `done` stage,
empty finding list or current passing checks alone cannot establish native
execution, original skill loading, semantic TDD, independent review, human
approval or a hooked task commit. The first unfinished stage does not enumerate
all later-stage obligations. Small-task history does not establish a resumable
workflow stage. Preserve original contracts, independent reviews and final
hooked Git/check requirements.

Offline regressions check full snapshots before checking delta reconstruction,
unchanged responses, check-only and policy changes, original finding references,
raw records, baseline rejection and storage/output limits. A smaller serialized
delta demonstrates fewer response bytes in that fixture; **actual model token
savings and quality equivalence remain unmeasured**. No new paid pilot is needed
to run these tests. See [coordinator handoffs](coordinator-overhead.md) and the
[compact guide](compact-context.md) for the rest of the opt-in workflow.
