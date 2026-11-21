# Compact mode (explicit opt-in)

Use this reference only when the developer requests compact mode or starts a task with
`kitsu task start "<request>" --host <host> --compact`. The host remains the coordinator.
This mode changes observations and handoffs, not the workflow's reviews or approvals.

## Repository evidence

Choose observations selectively within the host's existing tool permissions:

- Prefer a native host read when the entire short file is needed, and a scoped native
  search when its output is small. A compact envelope can cost more than that source text.
- Use typed `kitsu context read` and `kitsu context search` for broad or noisy retrieval,
  and `context collect` for independent batches. Narrow paths and line ranges first.
- Use compact declared-check output for noisy checks, then expand the retained logs
  needed to understand an outcome.

Do not route every read or command through the collector. The existing `--compact`
opt-in selects this policy; there are no separate component switches.

Typed commands use native operands; Go builds the request:

```sh
kitsu context read internal/server.go --start 10 --end 60
kitsu context search 'register' internal
kitsu context search 'register[A-Z]' internal --regex
```

Paths are repository-relative. Search is literal by default; use `--` before a
pattern/path beginning with `-`. A single `-` is literal, not stdin. Items are
`source` for read and `matches` for search. Saved worker packets already contain
this syntax and the selective policy. `--help` gives exact options without setup.

Batch independent operations with `kitsu context collect --request <file|-> --json`.
The advanced request is JSON, not executable code:

```json
{"version":1,"operations":[
  {"id":"routes","kind":"search","path":"src","pattern":"register"},
  {"id":"config","kind":"read","path":"README.md","start":1,"end":30},
  {"id":"changes","kind":"git"}
]}
```

Search is literal unless `regex:true` requests Go regular expressions. Use original source
paths and line numbers. A common path can be on the item instead of each line. Read errors,
partial status, skipped counts and omitted counts before drawing a conclusion.

Use `kitsu context show <artifact-id> --item <item-id> --cursor <next_cursor> --json`
to expand evidence. Omit the cursor for an item's first page. Cursors belong to that artifact
and item. Check previews start expansion at the original log's beginning, even when their
diagnostics came from its tail. Expand until the evidence needed for the decision is present;
an omitted result is not an absent result. Narrow future requests instead of loading the
whole repository. Prefer a native host read when an entire short file is needed.

The collector returns at most 16 KiB by default. Increase `--max-bytes` deliberately when
required metadata cannot fit. Stale, evicted and expired artifacts require fresh collection;
expanding a log never runs its check again or restores an expired log. Artifacts cache local
rendered text; they are not the provider's prompt cache. Count all request and response bytes,
including envelopes, saved worker instructions, original skill reads and every expansion,
when measuring. Neither local cache hits nor smaller previews prove model token savings.

## Fresh workers

For coordinator observations, take `kitsu task show <task> --compact` after a full
task folder or the first successful small-task preparation exists. Do not probe
state before that history exists. A successful `prepare`/`finish` already returns
its interval and check facts; request a view only for additional state you need.
Keep its complete sections and `snapshot_id`; after relevant events use
`kitsu task show <task> --since <snapshot-id>`. Replace each returned section in
your saved view; keep omitted sections unchanged. Explicit empty collections or
`status:none` clear old facts. Keep the new id only after applying every change.
A missing/expired/mismatched baseline requires a fresh full snapshot. Never use
a bare delta after losing its base. Do not poll unchanged state in a tight loop.
The response defaults to 16 KiB and refuses a budget that cannot hold complete
sections; increase `--max-bytes`. Expand original path/line or command references
for findings, blockers and prerequisite details; do not treat omitted bodies as
absent problems. Worker-return references remain untrusted claims, distinct from
open findings in canonical reviews. Small-task stage and review completeness
remain unknown. No snapshot or delta certifies completion, skill loading, native
execution or a gate pass; the normal original reviews and hooked Git finish apply.

Only the coordinator writes the specific assignment and runs:

```sh
kitsu task prepare <task> --role <role> --stage <stage> --round <round> \
  --host <host> --workflow full|small --assignment <file|-> --dispatch
```

Add `--view launch` to return only launch fields, inline warnings and
`full_dispatch`; default full v2 JSON remains. `kitsu task dispatch <dispatch-id>`
reads the original saved native dispatch without new evidence, locks or workers.
It preserves archived source state and fails on policy/skill/packet drift; it
grants no current pass. Older/non-native records lack a bound prompt and require
fresh native preparation after finishing or explicitly abandoning their interval.
Never preview a worker packet merely to recover its launch identity.

Assignments carry the complete original behavioral contract, role-specific work,
primary source references and required evidence. Keep coordinator scheduling,
budget supervision and state/interval instructions in the coordinator. Do not
paste this whole reference or the coordinator's managed skill into every assignment,
or require workers to reread them: the packet already contains their full role
and selective observation policy. Selected skills and explicit task references
are exceptions and must still be loaded in full; never trim user instructions.

The v2 JSON holds `dispatch_id`, `label`, saved `path`/`sha256`, `model`, `baseline`,
`selection_id`, `worktree`, `state` and `native`. Use `native.prompt`, `native.model`
and `native.effort` on the native host's fresh-worker call, with no conversation
fork (`fresh_context:true`, `fork_turns:"none"`). On Claude Code also select
`native.role`. The generated prompt requires loading and verifying the saved packet;
`native_prompt_sha256` identifies that prompt. Do not print and reconstruct the packet.
Stop on unavailable models. Go prepares local data; only the host starts workers.

Without `--dispatch`, prepare retains its v1 reference contract: verify the saved
hash, then pass the full saved text unchanged, or begin the native prompt with `label`
and require reading/verifying the saved file before work. The unmanaged
`task brief ... --assignment <file|-> --save` and full-metadata `task brief ... --json`
protocols also remain compatible.

Keep original full role instructions and selected skills uncompressed. The saved text
includes original Codex role instructions once. On Claude Code use
native role `kitsu-team:<role>`; its definition supplies the role body once. Load all
selected original skills in every fresh worker; receipts do not prove loading.
Keep other native skills discoverable without preloading their bodies.

Preparation resolves current role/model/skill policy, writes skill receipts before
its baseline and blocks overlapping managed intervals in the worktree. It replaces
equivalent manual skill resolution/receipt writes and baseline capture; do not run
them again. Resolve coordinator policy and host capabilities once per current
policy/session. Refresh affected inputs and regenerate the packet after policy,
model, config, skill, test or check changes; a saved packet is not permission for
stale reuse. Use an available fallback for missing optional tools such as `rg`;
do not repeatedly retry or automatically install them.

QE packets snapshot bounded check/log/search argv recipes and scoped `rg` lookup
observations. Unknown or mismatched host/session/cwd/PATH scope uses the typed
search fallback directly; no hash computation or discovery turn is needed.
Full fitting failed logs have zero omissions; expand only needed omitted facts.
Use recipes within existing permissions; no lookup grants execution authority.

Prefer native completion notifications; otherwise use a long bounded native wait.
Avoid ten-second polling and time-pressure pings. Keep deadline/cancellation duties,
user progress updates and an independent host budget monitor active while waiting.
Do not add a CLI wait daemon, host rollout watcher or worker spawner.
Coalesce routine worker progress; preserve real questions, risks and blockers.
Use the native final return without a duplicate worker completion message when
it suffices. Only after terminal completion and required semantic red/review
decisions, sequentially batch `task finish` then `task prepare` in one host call
on success. A failed finish blocks preparation; failed preparation preserves
an already-finished predecessor. Never advance from a receipt or author claim.

After the worker has stopped, optionally ingest its structured return with
`kitsu task return <dispatch-id> --request <file|->`. The generated native prompt supplies
the exact schema and identities; the request is at most 64 KiB of UTF-8 JSON.
Minimal JSON has `version:1`, `dispatch_id`, `task`,
`packet_sha256` and explicit arrays `changed_paths`, `checks`, `findings`, `blockers`.
Paths are repository-relative; findings carry `severity` (`blocker`, `major` or
`minor`), `path`, positive `line` and `message`.
Check claims carry `id`, `state` and optional `run_id`; blockers are strings.
Copy the exact gate evidence state (for example `satisfied` or `failed`), not
the runner's `passed` outcome. Use `checks:[]` when no check evidence was observed;
this grants no passing-check authority. Omit optional claims unless needed;
`loaded.skills` names only selected packet skill ids/hashes, not support reads.
The optional `worktree` and `source` assert current state. Optional `loaded` and
`worker` reports remain unverified claims about loading, identity and completion.
Retain the raw host result when ingestion fails. Ingestion validates local bindings
and observed evidence; it releases no interval, executes no checks and grants no
review or approval authority. Structured return is optional; finish never requires it.

Then use
`kitsu task finish <dispatch-id> --expect-checks pass|fail|any` instead of retyping
the baseline for its write check. This replaces the equivalent manual `role check`;
do not run both. Finish judges existing records and runs no checks.
For defect QE work inspect a meaningful red and use `fail`; for refactoring QE
and green code/review/Git use `pass` when required. Run declared checks when
evidence is missing/stale. Default `any` only records the actual verdict and may
return `checks_verified:false`; it supplies no passing-gate authority.

Finish before writing the next role's receipts or coordinator records. A failure
leaves the interval active; correct through the responsible role, or use
`task finish <id> --abandon --reason <text>` to release it with no approval.
Private intervals expire after 24 hours and can then be abandoned. They are not
canonical workflow state, approval, review or proof that a native worker ran.

For an unmanaged saved brief, capture a baseline before the worker and run the
normal `role check --from <baseline>` afterwards. Shipped-change finalization keeps
its separate saved baseline and `--finalize` check.

Saved packets carry the worker's selective policy; use the original complete packet
once. For unmanaged briefs, include only that worker-facing policy: native short-file
reads, scoped search, ordinary quiet checks, collection for noisy retrieval and
retained-log expansion. Workers keep their existing tool
permissions. For a role without command execution, the coordinator collects evidence and
supplies it; never add tool access to make the compact commands available. Lookup can create
Kitsu-managed private context artifacts when its host permits the call; its repository source
and shared task-record write limits remain read-only.

Reviewers receive primary evidence and factual task state, never author opinions, excuses,
summaries, or adviser answers as review input. Do not forward author return prose or
claimed findings as independent review input; give source references and factual state.
Read-only cached context supplies no review
approval, check pass, skill loading, or completed-work authority. Small-task stage and round
must be supplied explicitly; saved history alone cannot resume that workflow.

Inspect the actual red failure for the intended missing behaviour; a claimed failure or
receipt does not establish semantic TDD. Keep independent reviews, role write bounds,
canonical reviews/approvals/task records and check freshness. Only the Git role makes the
final commit through the normal hooks and gate; private dispatch/return/finish records
never replace those requirements.

## Declared checks

Use ordinary `kitsu check run` for quiet checks. Use `kitsu check run --compact`
(or `--json`) for noisy outcomes and diagnostic excerpts. Do not collect a quiet
result, expand a complete result, or rerun checks solely to populate a handback.
The normal runner executes the same checks sequentially, retains logs under its existing
size/age limits, and records failures, timeouts, interruption and source changes normally.
Do not infer success from a preview. Read every outcome and warning, expand relevant failed
logs, and preserve the normal failure/rework loop. Context artifacts never replace passing
gate evidence. Only the developer chooses whether to change the workflow policy.
