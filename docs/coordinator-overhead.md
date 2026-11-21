# Coordinator dispatch and returns

Use [bounded state snapshots and deltas](coordinator-state.md) to inspect stage,
interval, checks, open recorded findings, policy and completion blockers without
reprinting complete records. These observations complement the handoff protocol below.

Kitsu can generate a native launch description and retain a structured worker
return. These helpers run locally in Go: they launch no model, change no role's
model/reasoning and grant no review or completion authority. Compact mode
remains opt-in:

```sh
kitsu task start "Fix login retries" --host codex --compact
```

Managed prepare/finish replaces equivalent manual baseline and write checks.
The unmanaged workflow keeps its manual checks and fresh skill resolution before
each worker. Original contracts, roles and selected skills remain complete.

## Generate the native handoff

Write a specific assignment before opening the interval. Keep it outside the
worktree, or create it in a coordinator-owned location before preparation.
Name the full contract, primary sources, write limits and required hand-back.
The packet supplies the original role and worker observation policy. Keep
coordinator scheduling, budget supervision and interval/state bookkeeping with
the coordinator; do not copy the whole task skill or compact reference into
assignments or ask each worker to reread them. Explicit task references and
selected skills must still be read in full. Assignment bytes remain unchanged.

```sh
kitsu task prepare fix-login --role developer --stage code --round 1 \
  --host codex --workflow small --assignment /absolute/path/code.txt --dispatch
```

Explicit `--dispatch` returns version 2; omitting it keeps the original version 1
response. Both save the complete assignment and order selections before baseline.

| Field | Use on the native host |
| --- | --- |
| `dispatch_id` | Identify the active interval for return and finish. |
| `path`, `sha256` | Locate and verify the complete original worker packet. |
| `native.prompt` | Pass this exact label-first message. It names the full packet and contains the required return shape. |
| `native.model`, `native.effort` | Apply the effective selection; stop if unavailable. |
| `native.fresh_context`, `native.fork_turns` | Require a fresh worker without coordinator conversation inheritance. |
| `native.role` | Select the `kitsu-team:<role>` native role on Claude Code. |
| `native_prompt_sha256` | Identify the generated prompt; this cannot prove equality to an encrypted host message. |

This is a launch description, not an executable command or universal host API.
The host makes the actual native worker call. Kitsu never invokes a model CLI
or a replacement subprocess worker. Codex's saved packet contains its original
role once; Claude's native role supplies its body once. Every fresh worker reads
the full packet and selected original skills, verifying hashes. Receipts alone
prove neither loading nor native execution.

## Retain a structured return

The generated native prompt supplies the exact JSON shape with real identities:

| Required field | Meaning |
| --- | --- |
| `version` | Return schema version, currently `1`. |
| `dispatch_id`, `task`, `packet_sha256` | Match the original assignment. |
| `changed_paths` | Claimed repository-relative source paths. |
| `checks` | Claims with `id`, `state` and optional `run_id`. |
| `findings` | Objects with `severity` (`blocker`, `major` or `minor`), source `path`, positive `line` and bounded `message`. |
| `blockers` | Nonempty bounded strings describing blockers. |

All four arrays are required, even when empty. An empty check list means no
claims, not passing checks. Optional `loaded` and `worker` objects retain claims
about packet/skill loading, host worker identity and completion. Optional
`worktree` and `source` assert current identity; workers need not run additional
commands just to fill them. Their permissions do not expand.

Start with the generated minimal shape and omit optional claims unless needed.
In `checks`, copy the exact gate evidence state, such as `satisfied` or `failed`;
the runner's outcome `passed` is a different field and is rejected here.
`loaded.skills` may name only selected packet skill ids/hashes. Reading a task
guide does not make it a selected skill; with no selected skills, omit `loaded`
or use an empty skill list. Retain the raw return when ingestion fails, then
correct the claim or use optional-return-free finish with actual evidence.

After the worker stops, retain its raw return outside the worktree and ingest it:

```sh
kitsu task return <dispatch-id> --request /absolute/path/worker-return.json
kitsu task finish <dispatch-id> --expect-checks pass
```

Use `--request -` for stdin. Input is at most 64 KiB of UTF-8 JSON. Unknown or
duplicate fields, trailing documents, unsafe paths, omitted arrays and mismatched
identities are rejected. `--max-bytes` bounds the response: mandatory claims,
findings and warnings are retained or the budget is refused. Keep the raw host
return if ingestion fails.

The response separates `claims` from independently `observed` local facts and
labels its authority `untrusted-worker-claims`. Kitsu checks current policy,
actual role write bounds and existing declared-check evidence. It does not
certify changed-path claim completeness, skill loading, native worker completion
or semantic review. Inspect the actual diff and primary evidence.

Ingestion saves a private `.return.json` beside stage records, leaves the
interval active and runs no checks. Identical retries require identical claims
and current observations. Changed source, policy, evidence or claims cannot
silently replace a saved return. Neither hashes nor private files are security
attestations against an actor with equivalent filesystem access.

Structured returns are optional; legacy finish callers do not need them.
Use `fail` only after inspecting a meaningful defect regression, and `pass`
where current green evidence is required. Default `any` can record unknown or
missing checks and grants no passing-gate authority. Finish before writing the
next role's selections or coordinator task records.

Use ordinary output for quiet checks. Run compact collection/checks only for
noisy output and expand needed evidence. Request a state snapshot after the task
folder or first successful small-task preparation exists, and only when its
additional facts are needed. Prepare/finish already returns interval/check facts.

## Avoid repeated coordinator work

Resolve stable host setup, role-list/configuration context and optional tool
availability once per applicable session/policy. Refresh affected inputs after
changes or errors. Managed preparation resolves each worker's current policy
itself. If an optional utility such as `rg` is missing, use an available fallback
rather than repeatedly failing or automatically installing it.

Prefer native completion notifications. Otherwise use a bounded native wait
compatible with host limits, cancellation, real deadlines and user progress.
Avoid ten-second polling, unchanged status reads and time-pressure pings without
an actionable reason. Keep the independent host budget monitor. Kitsu supplies
no wait daemon and does not infer completion from guessed rollout files.

Never pass a developer's full return or completion narrative to the independent
reviewer. Supply the original task, source/diff/test references, changed existing
tests and neutral check facts. Meaningful red tests, finding dispositions, human
approvals, original write limits, current evidence and the final hooked Git-worker
commit remain required. Preparing a Git packet does not complete the Git stage.

## Verify locally

The [focused delivery replay](benchmarks/compact-delivery-report.md) compares
repeated coordinator-guide delivery with packet reuse and ordinary quiet checks.
It verifies full original roles, selected skills, task facts, returns and all four
write-check finishes while counting every transport boundary. It is an offline
scripted comparison; real token savings and model quality remain unmeasured.

The replay runs real Kitsu commands in isolated Git fixtures with fake checks
and scripted workers. Its ledger charges exact canonical CLI payloads and the
fake host's declared JSON envelopes: assignments, full packet/skill reads,
returns, follow-ups, retries, expansions and receipts. It exercises review,
repair, fresh review and a real hooked local commit. Provider tokens and model
judgment remain unavailable, not estimated from bytes or call counts.

The previous ordinary handoff replay inserted assignment text into expected
facts without transporting it. That baseline is corrected here. Published old
receipts remain unchanged; their handoff figures do not measure complete native
dispatch. The new reference uses the same worker-facing contract and explicitly
declares its serialization. CLI calls are not coordinator model turns.

One verified local lifecycle produced the following
[replay summary](benchmarks/coordinator-overhead-replay.json). Both paths used
three new check executions, two evidence reuses and six worker returns.

| Scripted path | Recorded calls | Request bytes | Response bytes |
| --- | ---: | ---: | ---: |
| Clean v1 reference | 69 | 20,659 | 90,508 |
| Generated v2 plus retained returns | 77 | 25,175 | 121,038 |

The reference has no redundant role/skill introspection. V2 adds metadata and
return retention; its noisy-log recovery also charged six expansions, one
rejected request and one retry. It used more transport in this fixture. The
script does not simulate coordinator model wakeups or establish token savings.
Use the optional protocol where generated handoffs and retained claims help;
the compatible v1 path remains available. Payload lengths vary with temporary
paths and generated identities. All critical scripted facts matched, including
finding severity and actual final commit/gate checks. A separate fixture proves
that preparing Git after review cannot turn unfinished work into completion.

```sh
GOWORK=off go test -race ./internal/cli \
  -run 'Test(NativeDispatch|WorkerReturn|CoordinatorReplay|CompactSelectiveReplayRequiredFacts)' -count=1
GOWORK=off go test -race ./internal/roles ./internal/repocheck -count=1
```

Actual replay results and validation are recorded in the
[OpenSpec change](../openspec/changes/reduce-coordinator-overhead/tests.md).
No paid pilot, model calibration, paid reviewer or CI waiting is part of this
change. Real-task quality equivalence and model-token savings remain unproved.
