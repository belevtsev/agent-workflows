# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Native prompt | Deliver packet identity and minimal return shape once | Existing version 2 dispatch | no | Retain existing schema, hashes and host selection |
| Worker policy | Separate complete worker contract from coordinator supervision | Existing brief constraints and opt-in reference | no | Preserve exact assignment bytes and original selected skills |
| Offline replay | Count full delivery and quiet-check transport with equal required facts | Existing fake-host ledger and real local CLI/checks | yes | Existing lifecycle tests retain red/green, review/repair and hooked Git coverage |

## Rules

| Rule | Checked by |
| --- | --- |
| No automatic assignment deduplication or compression | test |
| Selected skills and explicit references remain original and complete | test, review |
| Ordinary start behavior, host model/effort and write permissions stay intact | test |
| Optional claims cannot replace current evidence or finish an interval | test |
| Byte reductions cannot establish model-quality equivalence or token savings | review |

Only worker-facing selection guidance changes; no daemon, model invocation,
new CLI switch, check runner or state-authority change is introduced. The host
chooses observations; Kitsu does not silently reroute explicit compact commands.
Historical evidence and the frozen paired study remain unchanged.
