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
`source` for read and `matches` for search. Give these examples to every fresh
worker using compact observations. `--help` gives exact options without setup.

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

Only the coordinator writes the specific assignment and runs:

```sh
kitsu task prepare <task> --role <role> --stage <stage> --round <round> \
  --host <host> --workflow full|small --assignment <file|-> --dispatch
```

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
native role `kitsu:<role>`; its definition supplies the role body once. Load all
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

Prefer native completion notifications; otherwise use a long bounded native wait.
Avoid ten-second polling and time-pressure pings. Keep deadline/cancellation duties,
user progress updates and an independent host budget monitor active while waiting.
Do not add a CLI wait daemon, host rollout watcher or worker spawner.

After the worker has stopped, optionally ingest its structured return with
`kitsu task return <dispatch-id> --request <file|->`. The generated native prompt supplies
the exact schema and identities; the request is at most 64 KiB of UTF-8 JSON.
Minimal JSON has `version:1`, `dispatch_id`, `task`,
`packet_sha256` and explicit arrays `changed_paths`, `checks`, `findings`, `blockers`.
Paths are repository-relative; findings carry `severity` (`blocker`, `major` or
`minor`), `path`, positive `line` and `message`.
Check claims carry `id`, `state` and optional `run_id`; blockers are strings.
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

Carry this selective observation policy into every fresh worker's assignment, including
native short-file reads, scoped search, collection for noisy retrieval and retained-log
expansion for checks. Workers keep their existing tool
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

Use `kitsu check run --compact` (or `--json`) for bounded outcomes and diagnostic excerpts.
The normal runner executes the same checks sequentially, retains logs under its existing
size/age limits, and records failures, timeouts, interruption and source changes normally.
Do not infer success from a preview. Read every outcome and warning, expand relevant failed
logs, and preserve the normal failure/rework loop. Context artifacts never replace passing
gate evidence. Only the developer chooses whether to change the workflow policy.
