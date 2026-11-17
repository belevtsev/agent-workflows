# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Offline preparation | Freeze revised binary, source patch, guides and original task | Existing manifest/isolated runtime | yes | Reuse immutable paired-v1 runner rather than modify live accounting |
| Quality evaluation | Recheck baseline/reference and grade outside solver | Frozen hidden oracle and original suite | no | Preserve prior task contract and grading inputs |
| Usage monitor | Capture every causal solver and enforce stop/reserve policy | Existing verified CLI/runtime/runner | no | Exact unchanged assets permit reuse without another paid probe |
| Posthoc export | Reconcile terminal sessions, categories, artifacts and approval identities | Retained results and separate analysis executable | yes | Existing original-packet/commit inspection remains read-only |

## Rules

| Rule | Checked by |
| --- | --- |
| Explicit owner approval precedes both measured calls | review |
| Production/guide inputs and historical experiments remain unchanged during execution | test, review |
| Each actual worker uses Sol/high and a fresh context | test, review |
| Missing/unfinished counters block another launch and remain unknown | test |
| Full protocol claims cannot be certified from encrypted dispatch bodies | review |
| Total tokens, uncached input and monetary cost remain distinct | test, review |

The source is a modified local build on main e5d8c17, not released main. The
generic paired runner's profile identifies topology/budget; separate frozen
metadata identifies the new source and executable. New analysis never replaces
the measured runner, production binary, prompts or grading assets.
