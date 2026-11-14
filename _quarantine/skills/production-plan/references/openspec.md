# OpenSpec integration

Use OpenSpec as the planning artifact workflow and [the plan standard](plan-standard.md) as the engineering content standard. Keep one canonical change. This adapter is informed by OpenSpec 1.14.1; inspect installed capabilities and current project configuration rather than copying that version's templates or requiring a frozen schema.

## Select the project and change

Automatic selection requires evidence owned by the scoped project: its initialized OpenSpec configuration/layout or a project configuration declaring a store. Inspect that evidence from the project root. A folder name, global CLI availability, or a machine-wide default store alone does not establish adoption. An explicit user selection can identify a change or store; retain its authorization boundary.

Inspect the resolved root and its provenance before reading change artifacts or writing anything. It must be the scoped project's OpenSpec root or the store that project declares or the user selected. A `global_default`, implicit, unexpected, or out-of-scope root cannot silently select the workflow or expand write scope. Continue a chat plan and explain a mismatch; ask only if the intended project/store is materially ambiguous. A declared external store can legitimately own artifacts outside the code repository, but does not authorize unrelated code changes.

Reuse a named change. Otherwise inspect active changes and existing specifications to establish whether the request continues existing work. Do not duplicate a matching change or choose among several plausible targets silently. Read the relevant specifications in full, including scenarios, before deciding what behavior changes. Record code revisions separately from the artifact/store revision when they differ.

## Inspect without effects

Use the installed CLI's help and read-only JSON commands from the verified project/root. Prefix each inspection process with `OPENSPEC_TELEMETRY=0 OPENSPEC_NO_COMPLETIONS=1`; do not persist these settings in global configuration. Inspect the version with `--version`, without `version --check`. Do not install a missing CLI merely to inspect a project.

Use only commands needed for the current change, for example:

| Inspection | Purpose |
| --- | --- |
| `openspec list --json` / `list --specs --json` | Active changes, capability inventory, resolved root/provenance |
| `openspec status --change <name> --json` | Resolved schema and completed, ready, blocked, or optional artifacts |
| `openspec instructions <artifact-id> --change <name> --json` | Actual instructions, template, output paths, existing outputs, dependencies |
| `openspec templates --schema <resolved-schema> --json` | Templates before a change exists, or when instructions are insufficient |

Use a store selector only when the installed command supports it and the store is established above. `templates` in 1.14.1 resolves from its working directory and does not accept `--store`: run it at the verified planning root and pass the resolved schema. Prefer the template embedded in artifact instructions. Do not assume default artifact IDs, filenames, working directory, or schema when the project overrides them. Use noninteractive options supported by the installed command; do not run update, completion installation, profile changes, or workspace-generating context options during inspection.

If the CLI is absent, configuration cannot resolve, required capabilities are incompatible, or the schema cannot hold the required engineering content, state the precise limitation and continue useful source investigation and a chat preview. Identify any necessary setup as an execution prerequisite; do not emulate successful OpenSpec validation or rewrite the project's schema to fit this skill.

## Map engineering content

Obey resolved artifact instructions, templates, dependencies, and project rules. For the default `spec-driven` schema, map the plan as follows; for a custom schema use its equivalent roles rather than forcing these files:

| Artifact | Engineering content |
| --- | --- |
| Proposal | Problem, motivation, affected users/operators, intended outcome, scope, accepted constraints and capability impact |
| Specifications | Observable requirements, success/rejection/failure scenarios, and compatibility expectations |
| Design | Revision-backed current-state evidence, decisions and alternatives, ownership/interfaces/data flow, affected failure/recovery behavior, validation matrix, consultation/review dispositions, prerequisites and blockers |
| Tasks | Dependency-ordered implementation and verification actions with observable completion evidence |

Put implementation mechanisms in design/tasks rather than inventing requirements. Respect optional artifacts. For changes with no spec-level behavior change, use the schema's supported opt-out (1.14.1 `skip_specs: true`) instead of manufacturing deltas. Choose that metadata only when writes are authorized. If design is optional and its production concerns do not apply, keep the necessary context and verification in supported artifacts; do not lose substantive decisions merely to omit a file. If instructions permit omission but status still lists that artifact pending, report the discrepancy rather than manufacture content or claim the completion flag passed.

Before revising an existing change, read its current artifacts and progress. Carry accepted decisions, relevant evidence, unrelated content, and completed task history forward. An explicit correction updates the affected decision and dependent artifacts; it does not erase the rest. If changed requirements invalidate earlier verification or require additional implementation, add unchecked follow-up work and label the old evidence's limit. Do not silently treat historical checkmarks as proof for the revised requirement. New implementation/verification work remains unchecked; writing a plan is not executing it.

Required consequential-decision consultation and independent design challenge still apply. Record their findings/disposition in the supported design or equivalent artifact. OpenSpec structural validity neither replaces those gates nor establishes plan completeness, executed tests, or production qualification.

## Deliver within the operating mode

**Plan Mode or chat-only:** read available configuration/artifacts, then deliver one complete chat plan describing its intended artifact mapping and evidence limits. Do not create a change, edit metadata, save files, or run any mutating OpenSpec command, even when the request asks to save. Missing tools may remain a clearly named execution prerequisite.

**Authorized artifact saving in a write-permitted mode:** after verifying initialization, root, schema, and target scope, use OpenSpec's supported change creation for a genuinely new change, explicitly selecting the established schema when its CLI default differs. Then create or revise only that change's planning artifacts and required metadata. Check that resolved output paths, including symlink targets, stay within the selected change's authorized artifact directory; an unexpected custom-schema output is a scoped limitation, not permission to write elsewhere. Follow the resolved dependency order and reuse current outputs. Honor authorization already granted rather than asking again for every artifact. Creating a new change must not bootstrap an uninitialized root implicitly.

Validate the selected change with the installed structural validator (for 1.14.1, `openspec validate <name> --type change --strict --no-interactive` from its verified root) and inspect status again. Resolve structural failures within authorized planning scope, or report the exact unverified limitation. Summarize saved artifact paths, material decisions, remaining unchecked tasks, and actual validation evidence; the artifacts remain canonical. A CLI suggestion to apply is a handoff, not permission to apply.

Stop at implementation handoff. Do not apply code changes, mark implementation tasks complete, sync deltas into main specs, archive, commit, publish, change profiles, install the CLI, or initialize projects as a consequence of planning. Those actions retain their own scope and authorization.

## Upstream references

- [CLI and root-aware workflow commands](https://openspec.dev/docs/cli)
- [Project configuration](https://openspec.dev/docs/project-config) and [spec-driven artifacts](https://openspec.dev/docs/schemas/spec-driven)
- [1.14.1 root selection](https://github.com/Fission-AI/OpenSpec/blob/v1.14.1/src/core/root-selection.ts) and [template resolution](https://github.com/Fission-AI/OpenSpec/blob/v1.14.1/src/commands/workflow/templates.ts)
- [1.14.1 CLI hooks](https://github.com/Fission-AI/OpenSpec/blob/v1.14.1/src/cli/index.ts), [telemetry](https://github.com/Fission-AI/OpenSpec/blob/v1.14.1/src/telemetry/index.ts), and [completion tips](https://github.com/Fission-AI/OpenSpec/blob/v1.14.1/src/core/completion-tip.ts)
