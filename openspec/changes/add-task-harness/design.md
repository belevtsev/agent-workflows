# Design

An atomic worktree-local record stores requested scope, starting index, working snapshot, actor-attributed results and evidence references. The Go transition function receives fresh local observations. Reads and both host integrations share its decision. Action tickets bind read-only results to a candidate. A pre-dispatch claim serializes independent review. Writing actions can return across a candidate change. Explicit scope extension resumes an earlier design.

The assertion runner executes an argv from the repository root and retains its log. Git content, original bytes, assertion identity and the deciding local check records determine readiness. The commit path uses ordinary hooks and compares the actual commit tree and parent after interruption. Native hosts own model execution, permission boundaries and requested remote writes. Explicit confirmation reads the configured push destination and, for a GitHub PR, the open head and base before finishing. Captured session identities and token notices are observations with potentially incomplete coverage.

A repository-check failure stays available on resume until a new result supersedes it. The assertion event preserves the original check ID, run ID, log digest, outcome and exit status when available. A retry can reuse matching passing evidence and complete with its own ticket. If evidence later becomes stale, a new ticket is required before running further checks.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Task engine | Select a scoped action from task events | Local candidate and evidence | yes | Existing managed stage records describe worker intervals |
| Task commands | Record intent, claims, assertions and delivery | Engine, private stage ledger, runner | yes | Reuse existing snapshot, lock, logs and declared checks |

## Rules

| Rule | Checked by |
| --- | --- |
| A review and its findings identify the reviewed candidate | test |
| A verified task requires executable current evidence | test |
| Detachment preserves evidence by default | test |
