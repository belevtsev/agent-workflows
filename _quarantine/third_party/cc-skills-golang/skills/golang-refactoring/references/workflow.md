# Staging a multi-step Go refactor

Use this reference when the requested change has dependent stages, multiple consumers, or an API migration. A local rename or extraction can proceed directly. Existing user authorization determines editing and delivery scope; this workflow does not add mandatory approval, commit, merge, or PR steps.

## Map the affected behavior

Identify changed symbols, callers, method sets, package initialization, generated representations, and consumers outside the local build. Use available semantic tools where useful, then inspect reflection, tags, templates, and wire names separately.

For a migration with several stages, a small inventory makes ordering reviewable:

| Transform | Affected callers | Compatibility concern | Evidence |
| --- | --- | --- | --- |
| Extract validation | One caller | Error identity and evaluation order | Existing behavior tests |
| Move an exported type | Multiple packages/modules | Import path and type identity | Consumer builds, alias analysis |
| Replace a hot loop | Same API | Result equivalence and cost | Focused correctness cases and comparable benchmarks |

Keep behavior changes distinguishable from mechanical changes. Separate stages when that makes verification or rollback clearer; a fixed line count or one-PR-per-operation rule is not required.

## Order dependent changes

| Relationship | Consequence |
| --- | --- |
| One change requires a new package, interface, or alias | Put the prerequisite first. |
| Changes touch the same files, symbols, or callers | Sequence them or coordinate ownership explicitly. |
| A workspace rename reaches many packages | Determine its full write set before overlapping other edits. |
| Changes are independent and file-disjoint | They may be implemented independently if task and harness allow delegation. |
| A consumer receives updates through a published module | Include the publication and consumer upgrade order in the plan. |

Compatibility shims or type aliases can make intermediate states buildable. Document why each exists and when it can be removed. Do not remove a compatibility layer until the relevant consumers have actually migrated.

## Choose delivery from the task

A single coherent diff is often enough. For an explicitly staged delivery, use the repository's branch and review conventions; an integration branch is an option when intermediate states should remain together. Existing unrelated work must remain intact.

When delegation is authorized and useful, give each worker bounded file or module ownership, prerequisite revisions, expected evidence, and a reminder not to revert others' edits. Keep dependent work sequential. Do not hardcode an agent count or spawn agents merely to execute each inventory row.

Committing, creating PRs, publishing, or merging follows the user's authorization. An approved migration plan does not require repeated confirmation for its already-authorized reversible steps. Raise a new decision only when scope, compatibility, or an irreversible action goes beyond that authorization.

## Verify and carry forward evidence

Select checks from [verification guidance](safety-net.md) and repository requirements. Reuse existing evidence when the relevant code, inputs, toolchain, and conditions match. A failure requires understanding whether it came from this change, the environment, or a known baseline.

Record meaningful stage completion and any remaining consumer update or compatibility shim. For long work, a concise migration note can preserve that state. Add source comments only for intentional behavior or temporary compatibility that a future reader needs to understand; do not seed a plan throughout the source.

Complete the refactor when the requested structure and preserved behavior are supported by the required evidence, temporary migration work in scope is resolved, and any genuinely external prerequisite is stated accurately.

For specific mechanics, use [the transform catalog](catalog.md), [Go tools](go-tooling.md), and [package/API changes](structural.md).
