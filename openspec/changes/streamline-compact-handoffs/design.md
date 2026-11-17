# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Typed context adapters | Build validated read/search requests from native operands and flags | Existing context request decoder and engine | yes | Reuse collection, rendering and source rules rather than another reader |
| Saved dispatch | Save one canonical assignment and print small dispatch metadata | Existing effective roles, skills, brief policy and snapshot | yes | Existing brief preparation, original role text and receipt-before-baseline |
| Local stage interval | Serialize preparation/finalization and preserve original write baseline | Saved dispatch, worktree-private files and filesystem lock | yes | Existing role checker and content-bound evidence judge |
| Command surface | Help, completion and reproducible reference | Existing command metadata | no | Keep normal defaults and legacy brief JSON compatible |

## Rules

| Rule | Checked by |
| --- | --- |
| Typed reads/searches share canonical collection validation and source containment | test |
| Saved assignments preserve original instructions and bytes while dispatch output stays bounded | test |
| Selection receipts precede baselines and an active interval prevents another brief mutation | test |
| Finalization validates original Git and per-path raw write limits without rerunning checks | test |
| Changed policy, stale evidence, forged receipts and unsafe private storage cannot certify completion | test |
| Defaults stay opt-in and ordinary tests invoke no model or paid pilot | test |
| Local stage bookkeeping grants no semantic review, human approval or task-completion authority | review |

## Ordering and state

Validate arguments, assignment bytes and output bounds before recording a skill
selection. Preparation writes selections before taking a worker baseline, then
stores the canonical assignment privately. Return model/effort, label, path/hash,
selection and baseline without reprinting the role body or assignment.

Managed preparation refuses another open interval before mutating selection
receipts. Finalization verifies the saved dispatch and effective policy, compares
current Git and per-path raw-byte writes with its original baseline and judges existing check records.
It executes no check subprocess. Expected `pass`, `fail` or `any` distinguishes
green requirements, a current red regression and observation-only bookkeeping.
Failed validation leaves the interval active. Explicit abandonment preserves a
reason and releases it without claiming acceptable writes or code quality.

Private intervals are mechanical state for the current worktree, not a competing
OpenSpec history or a resumable small-task workflow. Native worker execution,
human decisions and semantic judgments remain outside this adapter. Missing
or corrupted local state is never interpreted as a completed worker.

## Safety and compatibility

Use bounded regular files, restrictive private permissions, contained paths,
atomic publication and kernel-backed locking. Bind saved assignments to their
hash, worktree and effective role/test/skill policy. Keep original instructions
and raw assignment bytes, with the required label first; Claude selects its
native role rather than duplicating that role body. A saved selection does not
prove the worker loaded a skill. Reviewer assignments still require primary
evidence and exclude author opinions and adviser answers.

The typed commands call the same decoder/engine as `context collect`; the
advanced JSON interface remains available. Do not change cache or gate identity
rules to suppress stale evidence. New worker dispatch/interval code has a
separate bounded size budget; existing engine and component budgets stay intact.

## Verification

Run local CLI/Git fixtures and fake check executables only. Cover native
operands, schema-like errors, path containment, paging, byte budgets, original
brief fidelity, receipt ordering, policy/source changes, tampering, failed
write checks, red/green expectations and abandonment. Preserve compact opt-in,
help without side effects, legacy hook ingress and old brief compatibility.
Complete build, vet, Linux race, three-target lint/vulnerability and record
checks. No pilot, model CLI invocation or token-saving measurement is part of this change.
