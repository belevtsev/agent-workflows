# Run compact workers with less discovery

Compact remains opt-in. Start it explicitly, then let the selected Codex or
Claude host coordinate the task:

```sh
kitsu task start "Fix login retries" --host codex --compact
```

These changes retain original roles, selected skills, behavioral tests, review,
approvals and the supporting gate. A subsequent [paid pair](benchmarks/response-cycles-paid-report.md)
records actual tokens and protected checks for one task, without establishing
general savings or quality equivalence. The [earlier investigation](benchmarks/compact-overhead-plan.md)
explains the observed response sequences; it is not a saving forecast.

## Prepare the fields needed to launch

The coordinator writes `qe.txt` with the complete original request, QE work,
primary sources and restrictions. Kitsu supplies the full original role and
selected skill references in the saved packet:

```sh
kitsu task prepare fix-login --role quality-engineer --stage tests --round 1 \
  --host codex --workflow small --assignment qe.txt --dispatch --view launch
```

`--view launch` requires `--dispatch`. It returns `native.prompt`, its hash,
host/model/reasoning, fresh-context/fork settings, packet path/hash, task and
dispatch identity, worktree and warnings. On Claude it also retains
`native.role`. Pass the native fields to the host's fresh worker directly.
The worker loads and verifies the complete packet and all selected original
skills. The coordinator need not preview or reconstruct the worker's role body.

Without `--view launch`, native preparation retains its full v2 JSON. The
launch response's `full_dispatch` command retrieves that original full response:

```sh
kitsu task dispatch <dispatch-id>
kitsu task dispatch <dispatch-id> --help
```

This is an archival read. It preserves the original state and prompt after
permitted worker source edits; it does not establish current check freshness,
completion or permission to reuse stale work. Current role/skill/adviser/check
policy drift, changed packet bytes or another worktree cause refusal. Repeated
reads create no locks, snapshots, baselines, check runs or workers.

Full retrieval requires a native dispatch saved with this implementation.
Older records or non-native preparation have no bound original native prompt;
the command refuses to invent one. Finish or explicitly abandon an existing
interval before a fresh native preparation when needed. Default v1 saved briefs
and unmanaged workflows keep their established behavior.

## Use the QE packet's recipes

Only QE packets add a small `execution_hints` object. Its `execution_hints.argv` fields are executable
argument arrays for ordinary checks, noisy compact checks, retained-log start/
continuation and scoped typed search. Substitute the indicated artifact/item/
cursor or search operands; never execute a JSON string as a shell program.

Recipes name all declared checks when the declaration is small. More than eight
IDs, oversized serialized operands or an unreadable declaration use the CLI's
all-declared-check default, rather than a partial check suite. Actual check
execution still validates the declaration and evidence normally.

`rg_lookup` is a local availability observation, not proof of execution or
authority. It carries fingerprints for host, known machine/session and actual
cwd/runtime/PATH context; it publishes no raw environment paths. Fresh-worker
scope can be unknown or different. A removed, replaced or unverifiable cwd
leaves the observation unknown, including on macOS. Use `search_fallback`
directly when scope cannot be verified; workers need no hash computation or
discovery turn.
On a relevant environment change or execution error, revalidate or use the
available fallback. Do not retry missing tools or install them automatically.

Preparation snapshots hints once. Re-reading a packet keeps those bytes;
selected skill/policy freshness and original packet hashes still apply.
Other roles do not receive this QE command inventory.

## Inspect check evidence before deciding

Use ordinary output for quiet checks. Reserve compact output for noisy results:

```sh
kitsu check run
kitsu check run --compact
```

A failed compact result now includes all chronological evidence when its
complete final JSON fits `--max-bytes`, including escaping, metadata and newline.
The default remains 16 KiB. An item with zero omissions and no continuation
needs no expansion merely because compact output was selected. Read the actual
assertions before calling a red meaningful.

Large results retain bounded diagnostic previews, explicit omissions and
original-log cursors. Expand only the evidence needed for a decision:

```sh
kitsu context show <artifact-id> --item <item-id>
kitsu context show <artifact-id> --item <item-id> --cursor <next_cursor>
```

Expansion reads the retained original; it never reruns the check. Missing,
expired, invalid or stale evidence stays explicit. A small raw log can still
exceed the budget after JSON escaping. Multiple checks share one response
budget while retaining all required outcome metadata. Passing logs remain
quiet and expandable.

## Keep handoffs sequential and conditional

Wait for terminal native completion. Coalesce routine worker-to-coordinator
progress while preserving user updates, real questions, risks and blockers.
A progress notification is not a completed worker. Keep cancellation, deadlines
and the independent host budget monitor active during bounded native waits.
Workers should use their native final return for completion without a duplicate
completion message when that return suffices.

Only after the coordinator has inspected the original meaningful red, the QE
worker is terminal and the developer assignment is ready, the two existing Go
CLI commands may share one host call:

```sh
kitsu task finish <qe-dispatch-id> --expect-checks fail &&
  kitsu task prepare fix-login --role developer --stage code --round 1 \
    --host codex --workflow small --assignment developer.txt --dispatch --view launch
```

Replace placeholder IDs and filenames with actual current values. The second
command runs only if the first succeeds. Group other dependent local steps
the same way; formatting failure must not launch a check. A failed finish
leaves its interval active. If preparation fails after a successful finish,
report that partial transition and retain the finished record.

A receipt does not certify semantic TDD or independent review. Before review
or Git handoff, inspect actual findings and dispositions. Reviewer assignments
contain primary source/diff/tests and neutral check facts, never author
conclusions or excuses. Preserve fresh independent review, original selected
skills, write-bound validation and the Git worker's normal hooked commit with
current passing checks and a clean tree. Structured returns remain optional
untrusted claims; an expected-red failure is check evidence, not automatic
permission to discard a genuine finding.

## Offline verification and limits

The new regression cases cover exact N/N−1 serialized budgets, quote escaping,
UTF-8, shared multi-check results, missing/large original logs and exactly-once
checks; bounded recipes and actual cwd/PATH scope; both-host launch/full
roundtrips, prompt immutability and mutation-free archival reads; and lifecycle
equivalence through red, a major finding, repair, fresh review and a hooked commit.

Offline fake-host calls and payload bytes are proxy measures. They do not
establish actual provider-token savings, prices, model compliance or general
quality equivalence. Existing ordinary/full response contracts remain available;
no model downgrade, automatic stage advance, worker spawner, wait daemon or
new paid pilot is introduced.

See [compact observations](compact-context.md), [coordinator handoffs](coordinator-overhead.md)
and the [OpenSpec record](../openspec/changes/reduce-compact-response-cycles/proposal.md).
