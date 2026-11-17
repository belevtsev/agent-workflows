# Design: add-compact-context

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Go collector | Validate versioned reads/search/Git operations, execute four bounded read workers, preserve ordering | Git snapshots and contained filesystem reads | yes | Existing gitx; all subprocesses retain execution boundaries |
| Private artifacts | Retain complete eligible context or log references, exact-budget pages, TTL, oldest eviction and freshness | Go collector | yes | os.Root, fslock; independent of gate installation/store removal |
| Worker brief | Resolve model/reasoning, original skills and allowed advisers, then receipt and baseline | Private artifacts, existing roles/skills/task readers | yes | Existing selection receipts and immutable task policy |
| Compact check adapter | Observe normal sequential execution, retain outcomes and preview/log references | Existing runner and evidence; Private artifacts | yes | The same runOne, store records, content keys and runner limits |
| CLI integration | Explicit opt-in, help and completion; preserve context-hook ingress | Worker brief, Compact check adapter | no | Existing stdlib flags and shared UX metadata |
| Research and documentation | Explain operational contracts, primary-source tradeoffs and offline/live evaluation | CLI integration | yes | Existing token accounting and evaluation; no invented usage |

## Rules

| Rule | Checked by |
| --- | --- |
| Collection accepts only declared bounded operations on contained eligible sources | test |
| Required outcomes and uncertainty survive response limits with explicit expansions | test |
| Artifacts remain private, bounded, isolated and fresh across raw-source/policy/evidence changes | test |
| Every fresh worker receives current instructions and a baseline after its skill receipt | test |
| Compact checks retain normal sequential outcomes and full existing retained logs | test |
| Default behavior and legacy hook ingress remain compatible | test |
| Offline comparisons charge request/response/page overhead without inventing tokens | test |
| Reviewer independence, human approval and host tool authority remain unchanged | review |

## Storage details

Requests accept 1–32 operations with unique bounded IDs, clean repository-relative paths,
inclusive read ranges, literal searches or explicit Go regexes. Only regular nonignored UTF-8
sources without NULs, at most 1 MiB each, are read. Per-operation results are bounded to 1 MiB.
Partial searches preserve skipped counts. Independent reads use four joined workers; checks
remain sequential because their filesystem effects are not assumed independent.

Artifacts live under each GitDir/kitsu-context, with 0700 directories and 0600 files where the
platform supports those modes. They expire after 24 hours, have a 32 MiB individual bound and
64 MiB aggregate bound with serialized oldest-entry eviction. Paths are contained by os.Root.
Checks keep only previews and references to the runner's retained logs, avoiding duplicate
large logs and per-physical-line amplification. Complete retained text expands in 1 KiB chunks.

Default responses are at most 16 KiB, including JSON framing and newline. Required metadata,
outcomes and uncertainty survive all budgets; inadequate budgets fail explicitly. All
omissions have cursors; check previews restart expansion at the original log's beginning.
Single-source paths move to the item to reduce repeated JSON. Expansion never executes checks.

## Freshness and authority

Snapshots include normalized Git trees, HEAD, eligible raw source bytes and effective ignored
tracked paths. Raw bytes prevent clean-filter/CRLF stale hits. Artifact publication rechecks
the source state. Brief reuse re-resolves external selected skills and effective policies.
Check reports bracket verdict/log collection with evidence hashes and preserve the earliest
deciding-record expiry; source tree and final verdict must agree. Context is an observation,
never current check evidence, approval, skill-loading proof or a completed-work record.

Only the coordinator prepares briefs. Each writes the existing skill receipt before capturing
the baseline. Codex receives mandatory role instructions; native Claude roles supply those
instructions once. Reviewers receive factual state and primary references, no author opinions
or adviser answers. Small-task stage/round remain explicit. Roles keep original tool access.

## Verification and rollout

Keep normal command defaults and legacy hook behavior. Add a lazily loaded task-skill reference
without raising prompt budgets. Test output bounds, error cases, byte preservation, isolation,
cache invalidation, baseline ordering, check equivalence and freshness. Existing CI runs native
Linux/macOS/Windows; local cross-compilation is not a native-runtime claim. Add a separate
2,000-line production budget without raising existing component limits. Offline benchmarks
include overhead and all required expansions. Paid live comparisons require a separate owner
decision; no release-wide default adoption follows from byte reductions alone.
