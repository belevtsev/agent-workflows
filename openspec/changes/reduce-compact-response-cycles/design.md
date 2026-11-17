# Design

## Approach

Fit-aware check rendering first attempts a complete chronological failed-result
page using actual JSON serialization, including metadata, escaping and newline.
Retained-log collection streams bounded candidates and shares their allowance
across checks. A large or unavailable original remains an explicit preview/
partial result with existing recovery semantics; checks never rerun to expand.

QE preparation snapshots five argv templates from current command/flag metadata
and declared check IDs. More than eight IDs or over 256 aggregate serialized
operand bytes uses all-declared-check operands, never a partial suite. Optional
`rg` observations use local lookup without execution; fingerprints include host,
machine/session when known and actual cwd/runtime/PATH context. Unknown or
mismatched scope uses the typed search fallback. Verification reuses saved hints
bytes instead of taking a new environment snapshot.

The coordinator keeps required semantic red/review decisions and terminal native
completion outside receipt authority. Existing finish then prepare can share
one host call only on success. Failed finish blocks preparation; a later failed
preparation preserves its already-finished predecessor. Routine worker messages
may be coalesced while actionable questions, risks, blockers, cancellation,
deadlines, user progress and budget supervision remain.

The default native full response stays version 2. An explicit launch projection
keeps exact prompt/hash, packet/dispatch/task identity, worktree, launch settings
and warnings inline, plus a full-dispatch command. The original prompt template
is bound in the content-addressed stage record before its schema dispatch-id
slot is filled. Full reads reconstruct the saved dispatch after validating
private record/packet identity, original role/skill/adviser/check policy and
worktree binding. They do not take snapshots, create locks, run checks or spawn
workers. Authorized source edits do not replace the archived original state.
Old/non-native records without a bound prompt require fresh native preparation.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Fit-aware rendering | Return complete fitting failures or bounded recoverable previews | contextpack.Render and retained-log streaming | no | Existing exact serialization, original log reader and pagination retain one runner and evidence authority |
| QE packet hints | Snapshot bounded current recipes and scoped availability observations | Existing UX registry, declarations and canonicalWorkerBrief | yes | Existing help metadata supplies syntax; original packet supplies role/assignment/skill identity |
| Native launch view | Project mandatory launch fields and read the saved full dispatch | Existing content-addressed stageRecord and packet verifier | no | Keep the existing native launch formatter and interval storage; bind its original prompt instead of adding a second execution path |
| Host cadence | Guide terminal and semantic decisions before sequential local operations | Existing coordinator guidance and lifecycle replay | no | Existing finish/prepare commands and fake-host lifecycle avoid a new controller or spawner |

## Compatibility and limits

Only explicit compact helpers select the new observation path. Quiet ordinary
check output and full dispatch defaults remain. The worker implementation budget
grows from 1,200 to 1,400 lines for bounded QE hint helpers; gate and unrelated
subsystem limits remain unchanged. Coordinator text stays within 12,000 bytes.
Replay call/byte measurements are proxies, not provider-token or quality proof.

## Rules

| Rule | Checked by |
| --- | --- |
| Final serialized responses share the requested budget and preserve outcomes | test |
| Complete fitting failed logs retain order; noisy or unavailable originals stay explicit | test |
| Original-log expansion never reruns checks or manufactures fresh passing evidence | test |
| QE recipes remain bounded and valid; tool lookup grants no execution authority | test, review |
| Actual cwd/runtime scope and unknown fallbacks preserve immutable packet bytes | test |
| Launch and full views preserve original role/assignment/skill and native settings | test |
| Full-reference reads validate identity and policy without new evidence or state writes | test |
| Terminal and semantic decisions precede conditional handoffs; failed steps remain explicit | test, review |
| Compact stays opt-in with original review, approval and normal hooked Git authority | test, review |
| Offline metrics introduce no paid pilot or token/quality claim | test, review |
