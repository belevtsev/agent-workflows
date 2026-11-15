# Design: add-existing-skills

Skills remain owned by their original source. Kitsu inventories local metadata, saves role
references, and resolves a new worker's selected sources. The coding-agent host owns execution.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `internal/skills` | Discover local host catalogs, validate metadata and availability, resolve original paths and hashes, and preserve portable selection receipts | `internal/fslock` | yes | Existing `internal/roles` owns role definitions and strict TOML but not host skill catalogs; `internal/advisers` history is consultation-specific and must not become a skill runtime |
| `internal/roles` | Parse layered skill bindings, edit the chosen scope, report selected references, and retain model and write-limit behavior | none | no | Existing configuration diagnostics, lossless role editing, path checks, and model defaults |
| `internal/cli` | Expose skill commands, validate role selections before writes, record task selections, and extend full/small structural checks | `internal/skills`, `internal/roles`, `internal/change` | no | Existing command parsing, JSON output, repository lookup, and change-record integration |
| `internal/change` | Continue checking requirements, task mapping, review ledgers, and stage content | none | no | Existing structural and content-id rules; skill receipt checks stay beside the existing CLI record integration |
| `internal/fslock` | Serialize receipt mutation for one task across local writers | none | no | Existing bounded cross-platform file locks rather than a new persistent task-state lock |
| Host instructions and generation | Resolve before each fresh role, load original instructions, add Claude `Skill`, and retain role limits | `internal/roles` | no | Existing coordinator brief, generic role texts, generated role files, and prompt budgets |

## Inventory and references

Discovery reads supported repository, personal, and installed-plugin layouts for one host. An
entry exposes its source scope, qualified id, original `SKILL.md`, metadata, SHA256 hash,
availability, and diagnostics. Parsing uses real YAML frontmatter, including quoted and folded
strings. Host-specific metadata defaults do not turn invalid metadata into a selectable entry.
Discovery starts no subprocess and executes no skill code. Original sibling files remain in place.

Qualified ids distinguish the host, source scope, and relative source identity. A short name must
resolve uniquely. Absolute references can select existing files directly. Repeatable `--root`
accepts a skill directory or collection; custom selections retain an absolute local reference
so a later handoff can find the original. Personal scope is preferable for machine-specific
references. Explicit roots establish local sources, not native plugin installation or trust.

Supported host configuration is respected when observable, including disabled skills and plugin
enablement. Codex name/path rules are ordered, user settings precede applicable repository
settings, and bundled disable policy applies to `.system` sources. Codex sidecar
`agents/openai.yaml` policy can prevent implicit invocation; unreadable or malformed supported
policy disables the affected source with diagnostics. Unsupported catalogs are disclosed.
`disable-model-invocation: true` is manual-only
and rejected by automatic role selection; `user-invocable: false` alone can supply background
knowledge. Listing an entry never grants its `allowed-tools` or runs a named script.

## Configuration and editing

Both role files are version 1 TOML. Personal `roles.toml` accepts only host/role skill arrays;
repository `kitsu-roles.toml` retains paths, specialist models and efforts, and adds skill arrays.
Each repository array replaces the complete personal list, including explicit `[]`. An omitted
array inherits. No configuration means no Kitsu-pinned skill; host-native availability remains.
The coordinator accepts skills only and keeps its host-session model and effort.

`roles set --skills` validates each reference before saving and retains existing model/reasoning
switches. Empty input writes `[]`; omitted input preserves the current override. Repository
reset removes that role/host's model, effort, and skills; global reset removes only personal
skills. `--global` never writes model or effort. Other roles, hosts, paths, and comments remain
preserved by the existing editor. Role tables and JSON expose skills beside existing selections.

## Resolution, records, and host handoff

`skills resolve` reads the current selected originals and returns paths and hashes without
loading them. Without a task it is read-only. With a task, workflow defaults to `full` and needs
an existing change folder; `small` is explicit. Deterministic immutable SHA256-named version 1
receipts live in `openspec/changes/<task>/skills/` or `.skills/<task>/`. They contain task,
workflow, host, role, selected ids, scopes, hashes, and `load_status: unverified`, without source
bodies, absolute paths, or timestamps. A repeated identical selection can reuse its receipt;
changed sources or selections retain a new one.

Full resolution moves the entire small history only when its full destination is absent.
Competing histories fail. Record writes stay confined to the repository and refuse symlink
paths. `change check` discovers full and small receipts and validates them offline, without
re-reading personal settings or installed skill sources. It proves structure, not loading.

Before every fresh worker, the coordinator resolves its role, includes selected ids, original
paths, and hashes in the brief, and directs the worker to load original instructions through a
native skill tool or explicit reading. Coordinator receipt writes precede the worker's
write-check baseline. Claude role tools include `Skill`; Codex continues using
host delegation with role text. Skills supplement expertise without changing permissions,
writes, independent review, finding rules, or human approvals. No live host behavior is claimed.

## Rules

| Rule | Checked by |
| --- | --- |
| Discovery reads original local files and executes no skill, host, or model process | test |
| Host-specific metadata, qualified identities, observable disable policy, and manual-only restrictions are validated before selection | test |
| Repository skill arrays replace personal lists, including explicit empty arrays, without changing model defaults | test |
| Editing keeps omitted settings, rejects invalid bindings before writing, and respects skills-only global/coordinator scope | test |
| Resolution returns current original paths and hashes and leaves loading unverified | test |
| Shared receipts omit bodies and machine absolute paths, preserve immutable history, and support safe one-way promotion | test |
| Full and small record checks work without installed skill sources | test |
| Generated host files include skill handoffs and Claude `Skill`; role texts stay generic and within existing budgets | test |
| Skills preserve write limits, reviewer independence, finding rules, and human approvals | review |
| README examples, command help, task mappings, links, and changelog match the implementation | test |

Existing core, workflow, adviser, and prompt budgets stay unchanged. Skill production code has
a separate 2,000-line budget. Automated checks establish file and configuration behavior; a
receipt and correct instructions do not establish that a host loaded or followed a skill.
