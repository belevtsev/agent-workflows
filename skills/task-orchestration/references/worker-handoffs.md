# Worker handoffs and independent review

Use this contract for delegated repository work in any project. Its instructions
choose tools and check commands; keep briefs, results and records outside source
and managed releases. The coordinator selects the runtime agent, resolves its
model from policy, dispatches it and verifies actual identity and returned work.
Native commands validate declarations and evidence. They do not launch or stop
agents, authenticate identities, isolate permissions or authorize external actions.

## Assign before dispatch

Start the task and, for substantial work, attach a resolved before checkpoint:
`cw task assign` refuses a substantial task until then. A small task's mandatory [test-author dispatch](test-author.md) alone adds neither
substantial adviser checkpoints nor the substantial final-review gate; it still
retains native assignment, handoff, acceptance and selected execution checks. Set the plan's
`coordinator_id` to the actual coordinating agent; new tasks default to `/root`.
Prepare a brief with a unique immutable assignment ID:

```json
{
  "id": "component-change",
  "agent_id": "/root/implementer",
  "role": "implementation",
  "goal": "Implement the agreed component behavior",
  "owned_paths": ["src/component"],
  "constraints": ["Preserve unrelated changes and the agreed interface"],
  "check_ids": ["unit"],
  "stop_condition": "Return changes, source evidence, actual checks and unresolved questions"
}
```

```sh
cw task assign --task /private/task --input /private/brief.json
```

The emitted record retains the brief's task ID, exact source snapshot and SHA-256.
Give the worker that emitted assignment, relevant input and the result contract.
Distinguish runtime identity from assignment ID: successive assignments use new
IDs. Avoid passing the full conversation when a bounded brief is sufficient.

Roles are `implementation`, `evidence`, `review` and `verification`. Only
implementation results can declare changes. `owned_paths` contains clean relative
files or directory prefixes; `.` covers the whole project. Active implementation
assignments cannot own overlapping paths. Shared-filesystem workers must preserve
others' edits. `check_ids` selects commands already in the immutable task plan.
Use `cw task run` to retain actual execution; a worker's assertion is not a pass.

A `test_author` resolves the policy's `testing` model role but uses native
`role: "implementation"`, exact test-artifact paths and `check_ids: []`. This
permits handoff of a correctly failing regression without claiming a passing
execution. Keep real checks in the plan with coordinator or `verification`
ownership. Scope helpers, fixtures and behavioral data as well as test files;
do not give a coder a directory that contains the author's owned files.

Record contract ID/revision/digest, allowed inputs and feedback limits in existing
brief constraints. Do not add unsupported native JSON fields. The author receives
only its bounded assignment and approved packet, not full task history or raw
check logs. Its summary or referenced artifact retains requirement-to-case
mapping, inputs read, authored patch hashes, questions and any exposure. See the
[test-author contract](test-author.md) for semantic edits and replacement rules.

Use exact on-disk path spelling without symlink components. Existing direct file
aliases are checked for active ownership; hardlinks hidden beneath separate
directory scopes remain outside these declaration checks.

Scoped freshness hashes all entries beneath each owned directory, rejects
symlinks and limits the walk to 30,000 entries and 64 MiB. Prefer exact source
files when a directory also contains caches or linked resources. Whole-project
scope (`.`) uses the project's eligible-source snapshot instead.

## Return and accept evidence

The result JSON contains:

| Field | Meaning |
| --- | --- |
| `task_id`, `assignment_id`, `assignment_sha256`, `agent_id` | Exact task, brief and worker binding |
| `snapshot` | Full current `profile.snapshot` from `cw doctor --repo /absolute/project` |
| `status`, `summary` | `completed`, `blocked` or `cancelled`, with a concrete outcome |
| `evidence` | Source references with `path`, `start_line`, `end_line` and full-file `sha256` |
| `changed_files` | Declared changes with `path` and `sha256`, or `deleted: true`; empty for read-only roles |
| `check_ids` | Actual task check records cited by the worker |
| `findings` | Finding `id`, `summary` and source `evidence`; an empty array records none |
| `uncertainties` | Remaining unknowns; an empty array records none |

Do not invent a fingerprint or copy the original baseline after edits. The result
must match actual source at submission.

```sh
cw task handoff --task /private/task --input /private/result.json
```

The coordinator independently checks meaning, scope, source and results. Its
acceptance JSON contains `task_id`, `assignment_id`, `result_sha256`,
`coordinator_id`, `status`, `reason`, `evidence`, `check_ids` and `findings`.
Copy the exact emitted result hash; `evidence` contains real current source
references. Status is `accepted`, `rejected` or `unused`. Account for every finding
with `finding_id`, disposition `status`, `reason` and source `evidence`. The coordinator can
reject a finding against evidence; reviewer agreement is not a veto or permission.
A finding disposition of `accepted` acknowledges an unresolved finding and blocks
completion. Use `resolved` for an addressed finding or `rejected` for a finding
that the coordinator rejects against evidence, each with a reason and references.

```sh
cw task accept --task /private/task --input /private/acceptance.json
```

Blocked or cancelled work can be closed as `unused` with an explicit rationale
after the coordinator stops or supersedes it. This does not satisfy requirements
or count as independent review. The worker must stop changing source before its
assignment is closed; these commands do not stop the runtime process.

## Review substantial changes

New substantial tasks whose source differs from baseline require an independent
substantive reviewer before final adviser context. It must differ from the
coordinator and implementation workers. Its brief uses `role: "review"` and
`owned_paths: ["."]` to cover the full resulting source. Give it the completed
diff, actual callers/contracts and selected check results. It returns the same
handoff shape with no changes; record the result and coordinator dispositions.

Complete implementation handoffs, acceptance and selected checks before recording
final review. Review and check execution can overlap while source is stable, but
accepted review must bind completed check and worker state. Then prepare
`cw task context --phase final`, perform the fresh final consultation with the
task's bound advisers (or record that none are configured), adjudicate it and run
`cw task verify`, following [Global workflow](global-workflow.md).

Source changes require a new current review. Later worker/check mutations can
also invalidate review and final adviser context. Use a new immutable review ID
and refresh affected evidence. Implementation handoffs can retain scoped evidence
after unrelated edits; final review must cover the whole current source.

The gate checks structure, bindings, current source, actual check records and
declared identity separation. The coordinator still verifies that an independent reviewer
actually investigated the change and that its conclusions are sound. Declared
changes are checked against scope and source; they do not prove that undeclared
edits never occurred or establish operating-system isolation.

## Compatibility and proportion

Task records use version 3. A record from an earlier release is refused; start a
new task with `cw task start`. No old record is silently upgraded. Small and unchanged
read-only tasks gain no mandatory review, but delegated work still needs complete
acceptance or explicit closure as unused.

Use parallel workers for independent ownership or questions, usually one or two
alongside the coordinator. Keep dependent steps sequential. Measure comparable
duration, total usage, rework and verified findings with `cw metrics`; more agents
or passing synthetic cases do not establish productivity gains.
