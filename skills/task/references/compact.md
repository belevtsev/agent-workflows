# Compact mode (explicit opt-in)

Use this reference only when the developer requests compact mode or starts a task with
`kitsu task start "<request>" --host <host> --compact`. The host remains the coordinator.
This mode changes observations and handoffs, not the workflow's reviews or approvals.

## Repository evidence

Use native operands for common reads/searches; Go builds the request:

```sh
kitsu context read internal/server.go --start 10 --end 60
kitsu context search 'register' internal
kitsu context search 'register[A-Z]' internal --regex
```

Paths are repository-relative. Search is literal by default; use `--` before a
pattern/path beginning with `-`. A single `-` is literal, not stdin. Items are
`source` for read and `matches` for search. Give these examples to every fresh
worker using compact observations. `--help` gives exact options without setup.
Native reads remain suitable for a short whole file.

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
whole repository. Native reads remain useful when an entire short file is needed.

The collector returns at most 16 KiB by default. Increase `--max-bytes` deliberately when
required metadata cannot fit. Stale, evicted and expired artifacts require fresh collection;
expanding a log never runs its check again. Neither cache hits nor smaller byte counts prove
model token savings.

## Fresh workers

Only the coordinator writes the specific assignment and runs:

```sh
kitsu task prepare <task> --role <role> --stage <stage> --round <round> \
  --host <host> --workflow full|small --assignment <file|->
```

The returned JSON holds `dispatch_id`, `label`, saved `path`/`sha256`, `model`
(model/effort), `baseline` and `selection_id`. Verify the hash. Have the native host
start a fresh worker from the full saved text unchanged, or begin its prompt with
`label` and require reading/verifying that file before work. Do not print and
reconstruct the packet. Stop on unavailable models. This command invokes no model.

The saved text includes original Codex role instructions once. On Claude Code use
native role `kitsu:<role>`; its definition supplies the role body once. Load all
selected original skills in every fresh worker; receipts do not prove loading.
Keep other native skills discoverable without preloading their bodies.

Preparation writes skill receipts before its baseline and blocks overlapping
managed intervals in the worktree. After the worker returns, use
`kitsu task finish <dispatch-id> --expect-checks pass|fail|any` instead of retyping
the baseline for its write check. Finish judges existing records and runs no checks.
For defect QE work inspect a meaningful red and use `fail`; for refactoring QE
and green code/review/Git use `pass` when required. Run declared checks when
evidence is missing/stale. Default `any` only records the actual verdict and may
return `checks_verified:false`; it supplies no passing-gate authority.

Finish before writing the next role's receipts or coordinator records. A failure
leaves the interval active; correct through the responsible role, or use
`task finish <id> --abandon --reason <text>` to release it with no approval.
Private intervals expire after 24 hours and can then be abandoned. They are not
canonical workflow state, approval, review or proof that a native worker ran.

For a saved brief without managed bookkeeping use `task brief ... --assignment
<file|-> --save`, followed by the normal `role check --from <baseline>`. The
original full-metadata `task brief ... --json` protocol remains compatible.

Carry compact mode into every fresh worker's assignment. Workers keep their existing tool
permissions. For a role without command execution, the coordinator collects evidence and
supplies it; never add tool access to make the compact commands available. Lookup can create
Kitsu-managed private context artifacts when its host permits the call; its repository source
and shared task-record write limits remain read-only.

Reviewers receive primary evidence and factual task state, never author opinions, excuses,
summaries, or adviser answers as review input. Read-only cached context supplies no review
approval, check pass, skill loading, or completed-work authority. Small-task stage and round
must be supplied explicitly; saved history alone cannot resume that workflow.

## Declared checks

Use `kitsu check run --compact` (or `--json`) for bounded outcomes and diagnostic excerpts.
The normal runner executes the same checks sequentially, retains logs under its existing
size/age limits, and records failures, timeouts, interruption and source changes normally.
Do not infer success from a preview. Read every outcome and warning, expand relevant failed
logs, and preserve the normal failure/rework loop. Context artifacts never replace passing
gate evidence. Only the developer chooses whether to change the workflow policy.
