# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Role configuration | Resolve the saved host, model, reasoning, and selected skills | Version 2 role settings and installed host preference | no | Reuse the role parser and discovery |
| Runner client | Pin a reviewed policy and authenticate job receipts | Machine registration, HTTPS, and Ed25519 | yes | Keep the job contract independent of host hooks |
| Task ledger | Reserve work and recover signed results for the same attempt | Existing task records and worktree lock | no | Reuse the daily task lifecycle |
| Candidate import and verification | Check the exact accepted files and apply only authorized role edits | Git snapshots, stored assertions, and runner results | yes | Reuse scoped Git staging and staged commit validation |
| Installer and Git gate | Preserve host preferences and validate push input | Release manifest, personal settings, and Git's hook input | no | Reuse the installers and existing hook rules |

## Rules

| Rule | Checked by |
| --- | --- |
| Configured work requires a signed receipt for the reserved job | test |
| Verification preserves the intake assertion and requires evidence for the current candidate | test |
| Workers cannot change protected or ignored paths | test |
| Resume and release require the stored attempt and independent runner authority | test |
| Runner registration requires acceptance for each supported platform | test |

The developer starts or resumes `task run` from either host or a terminal.
Kitsu decides the next action using the saved task and current candidate.
Every automated action sends bounded source bytes to the pinned runner.
The worktree claim is saved before dispatch. The ledger lock is released
while the runner works.

The runner owns process containment, credential separation and check
reporting. It retains a signed result for an attempt even if the CLI exits.
Kitsu validates the result against the exact job and current candidate before
applying a bounded patch or advancing the task. A saved import digest covers
interruption during application. Review uses a distinct, read-only worker.
Host hooks continue to record ordinary session telemetry.

The user's opening assertion remains required. The test writer may propose
another reviewed check. A red baseline needs the named behavioral failure.
During rework Kitsu reconstructs the pre-code bundle with the corrected
tests and repeats the baseline. Verification and review bind the current
assertions and candidate. A requested local commit uses the verified index.

Role settings remain in version 2 `kitsu-roles.toml`. Execution policy and
signing authority belong to the project runner and machine registration.
Installers preserve the existing personal default unless replacement is
explicit. Active tasks from the old local launcher require operator
reconciliation and a new supervised task.
