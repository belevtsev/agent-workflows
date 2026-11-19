# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Repository role policy | Resolve each configured host, requested model, effort and original skills | Existing role parser and skill discovery | yes | Preserve version 1 and role presets |
| Daily workflow | Save role assignments and choose the next scoped architecture, test, implementation, verification or review action | Daily harness, worktree ledger, Git and verification | yes | Retain current action tickets, native review hooks and commit evidence |
| Managed dispatch | Invoke a configured native worker for a prepared interval and bind its handback | Worker brief, OpenSpec view, stage ledger and shared native transport | yes | Reuse `task prepare`, `task return`, `task finish`, reviews and developer approvals |
| Native subprocess transport | Run pinned host/model/effort, bound cancellation and observe native results | Installed Codex and Claude Code CLIs | yes | Reuse process cleanup and configured host setup |
| Scoped Git delivery | Commit precisely the staged candidate authorized by explicit paths | Git gate, target, parents and current check evidence | yes | Reuse checked Git commit and interrupted commit reconciliation |

## Rules

| Rule | Checked by |
| --- | --- |
| A configured stage binds role settings, attempt identity and its candidate before starting a native worker | test |
| The test writer's baseline records the intended regression or the authorized passing case. Implementation preserves the accepted tests | test |
| A managed worker's handback stays distinct from developer approval | test |
| A worker or interrupted run cannot silently expand the approved staged candidate | test |

The daily harness retains a separate check observation for the pre-code baseline, verifiable against actual captured command output. Tests, verification and review refer to immutable candidate evidence. A worker claims no completion just by exiting.

Managed execution is an explicit action on a prepared interval. Its separate launch record is durable before the subprocess starts and is updated with the observed native session and final handback. Ledger locks cover local record transitions. A separate process lease prevents abandonment while the launcher is still supervising work. For configured review, the reserved host parent starts a fresh reviewer. The matching hook notices bind the reviewer to the exact dispatch and candidate. A returned handback remains subject to the normal write and check validation. Full-stage launch consults the current OpenSpec state. Architecture and test-writing prerequisites depend on the saved human approvals.

Configured Git receives a read-only delivery inspection. The caller supplies an exact file scope and explicit commit message. Kitsu checks the candidate, current gate evidence and installed hooks before attempting Git. The saved intent recovers an exact matching committed tree and parents after an interruption.

Offline coverage exercises the dispatch contract, authority boundaries and Git side effects in disposable repositories. Signed-in local host acceptance still needs both real transports and native review hooks exercised with authorized accounts and models.
