# existing-skills

## ADDED Requirements

### Requirement: Original local skill inventory

Kitsu SHALL discover existing local `SKILL.md` sources for Codex and Claude Code in supported
repository, personal, and installed-plugin layouts. Repeatable explicit roots MUST support an
existing skill directory or collection. Discovery MUST preserve original directories and
relative resources and MUST NOT install, copy, execute skill code, or start host/model processes.

Source: owner's existing-skill reuse request; accepted original-directory and local-only plan.

#### Scenario: Existing sources are listed without execution

- **WHEN** supported repository, personal, and installed-plugin skill sources exist for the selected host
- **THEN** list reports their original sources without modifying them or executing any skill, host, or model process.

#### Scenario: Explicit roots expose existing local sources

- **WHEN** a developer supplies a skill directory or collection with repeatable `--root`
- **THEN** inventory includes those original local sources without asserting native installation, enablement, or trust.

### Requirement: Validated skill identities

Inventory SHALL parse host-appropriate YAML frontmatter and report invalid entries with source
diagnostics. Each source MUST have a deterministic qualified id and SHA256 hash. A selected short
name MUST resolve uniquely; missing, ambiguous, or invalid selections MUST fail before saving
an invalid binding or handing work to a role. `roles set` MUST also refuse duplicate selected
sources before writing. Explicit local references MUST
continue to identify their original source on later resolution.

Source: accepted metadata validation, qualified identity, and fail-before-worker plan.

#### Scenario: Metadata retains YAML values and diagnostics

- **WHEN** sources contain quoted or folded metadata and a malformed or invalid entry
- **THEN** valid metadata is parsed correctly and the invalid entry is reported with a source diagnostic and is not selectable.

#### Scenario: Duplicate names need qualified references

- **WHEN** a selected short name matches multiple sources
- **THEN** selection fails as ambiguous while the exact qualified id selects only its named source.

#### Scenario: Invalid references do not change bindings

- **WHEN** a `roles set` selection contains a missing, invalid, or duplicate reference
- **THEN** it fails without saving a partial binding or dispatching a worker.

### Requirement: Observable host policy

Kitsu SHALL respect supported observable host-disabled skill and plugin enablement settings and
disclose catalog limitations. It MUST NOT claim a complete effective host catalog for opaque or
unsupported layouts. Manual-only `disable-model-invocation: true` entries MUST be listed as
such and refused by automatic role binding and resolution; `user-invocable: false` alone MUST
remain usable as automatic background knowledge.

Source: accepted host-policy compatibility, manual-only restriction, and visibility limits.

#### Scenario: Disabled sources cannot be attached

- **WHEN** supported host configuration disables a skill or its plugin
- **THEN** automatic role selection and resolution refuse that source with an availability diagnostic.

#### Scenario: Manual-only and background metadata stay distinct

- **WHEN** one source disables model invocation and another only disables user invocation
- **THEN** automatic role use refuses the manual-only source and permits the otherwise-valid background source.

### Requirement: Layered role bindings

Skill bindings SHALL be optional host/role arrays in repository `kitsu-roles.toml` and personal
`os.UserConfigDir()/kitsu/roles.toml`. A present repository array MUST replace the complete
personal list, including explicit `[]`; omission MUST inherit. With neither binding Kitsu MUST
pin no skill and MUST leave native host skill availability intact. Personal files MUST accept
only skill bindings. Coordinator tables MUST accept skills without model or effort overrides.

Source: accepted personal default, repository policy, explicit-empty, and coordinator plan.

#### Scenario: Repository skill arrays replace personal defaults

- **WHEN** a role has personal selections and a repository selection or explicit empty array
- **THEN** its effective list equals the repository array while an omitted repository array inherits the personal list.

#### Scenario: Empty configuration pins no skills

- **WHEN** neither scope configures skills for a host and role
- **THEN** no skill is pinned and existing specialist model defaults and native host skills remain unaffected.

#### Scenario: Personal and coordinator scopes stay limited

- **WHEN** personal configuration includes model, effort, or paths, or a coordinator table includes model or effort
- **THEN** configuration fails instead of expanding those scopes.

### Requirement: Explicit selection editing

`roles set` SHALL accept comma-separated `--skills` with a host and retain existing model and
reasoning switches. Omitted `--skills` MUST leave its override unchanged; `--skills ''` MUST
write explicit `[]`. `--global` MUST edit personal skills only and reject model/reasoning.
Repository reset MUST remove that role/host's model, effort, and skill overrides; global reset
MUST remove only personal skills. Editing MUST preserve other configuration and reject invalid
selections before writing. Custom-root selections MUST retain a usable original local reference.

Source: owner's CLI selection request; accepted clear, reset, preservation, and global semantics.

#### Scenario: Set clear and omission have distinct effects

- **WHEN** a developer sets a skill list, changes only a model, or sets an empty skill string
- **THEN** the commands respectively save the selected list, retain its override, or suppress inheritance with an empty array.

#### Scenario: Reset applies to the chosen scope

- **WHEN** a role is reset in repository or global scope
- **THEN** only that host/role's chosen-scope overrides are removed and remaining inheritance and unrelated settings are preserved.

### Requirement: Role selection reporting

Host-specific role tables and JSON SHALL show selected skills alongside model, reasoning, and
advisers, including coordinator selections with host-session model and effort. Reporting MUST
distinguish a configured selection from loaded instructions and MUST retain both hosts when no
host filter is given.

Source: owner's role/skill visibility request; existing role selection table contract.

#### Scenario: Role output exposes current skill selections

- **WHEN** a developer lists roles as text or JSON for one host or both
- **THEN** the output includes current skill references and existing model, reasoning, and adviser information without claiming the skill was loaded.

### Requirement: Original source resolution

`skills resolve <role> --host` SHALL validate the effective selection and return original
`SKILL.md` paths and current SHA256 hashes for a fresh worker brief. Without `--task`, resolution
MUST be read-only. It MUST execute no skill or host/model process and MUST report loading as
`unverified`. Changed, missing, invalid, host-disabled, or manual-only sources MUST NOT be
silently substituted.

Source: accepted original-source handoff, fresh resolution, and truthful loading evidence plan.

#### Scenario: Resolution returns current original sources

- **WHEN** a valid role selection is resolved without a task and a source later changes
- **THEN** each resolution returns the original path and current hash without a receipt write, loading claim, or process execution.

#### Scenario: An unavailable selection stops the handoff

- **WHEN** a previously selected source becomes missing, invalid, disabled, or manual-only
- **THEN** resolution fails before a fresh worker receives a substituted source.

### Requirement: Portable immutable selections

Task resolution SHALL preserve immutable version 1 selection receipts with task, workflow,
host, role, selected ids, scopes, SHA256 hashes, and `load_status: unverified`. Shared receipts
MUST omit machine absolute paths, source bodies, and timestamps. Full mode MUST default when a
task is given and require an existing task folder; small mode MUST be explicit. Receipts MUST
use full `skills/` or small `.skills/<task>/` history and retain changed selections without
rewriting prior receipts. Promotion MUST move the complete small history only to an absent full
destination and MUST refuse competing histories.

Source: accepted portable record, immutable source identity, and small-to-full history plan.

#### Scenario: A task selection records portable source identities

- **WHEN** a valid selection is resolved for an existing full task or explicit small task
- **THEN** an immutable receipt records its identities and hashes in the correct workflow history without machine paths, bodies, timestamps, or a loading claim.

#### Scenario: Changed selections preserve earlier receipts

- **WHEN** task resolution later observes changed source content or a different role selection
- **THEN** it retains the previous receipt and records the new selection without overwriting history.

#### Scenario: Small skill history promotes without merging

- **WHEN** full resolution follows a small history for the same task
- **THEN** the complete history moves only if its destination is absent and competing histories cause failure.

### Requirement: Offline history checks

`change check` SHALL validate saved full and small skill receipts, including named small tasks
without a full task folder, without installed sources or personal configuration. Malformed
receipts, conflicting histories, and unsafe or symlink record paths MUST fail. Structural checks
MUST NOT treat selection as proof of loading or require source availability on another machine.

Source: accepted portable CI validation and repository-confined record plan.

#### Scenario: Shared selections can be checked offline

- **WHEN** full or small receipts exist without the original local skill directories
- **THEN** global and named change checks validate their structure without host execution or source loading.

#### Scenario: Unsafe or malformed histories fail

- **WHEN** receipts are malformed, histories conflict, or record paths escape through symbolic links
- **THEN** record checks and mutations fail without following unsafe record paths.

### Requirement: Existing skill handoff

Coordinator instructions SHALL resolve current selections before every fresh role and give the
worker selected ids, original paths, hashes, and an instruction to load the originals through a
native tool or explicit read. Claude role files MUST include `Skill`; Codex MUST retain host
execution rather than add a runtime. Generic role texts MUST remain language-neutral. Skills
MUST NOT change role write limits, permission grants, independent review, finding rules, or
human approvals. Generated-file and prompt-budget checks MUST remain in force.

Source: owner's existing expertise request; accepted host handoff and retained role boundaries.

#### Scenario: Worker receives original skill instructions

- **WHEN** the coordinator starts a fresh role with selected skills
- **THEN** its instructions require resolving and briefing original sources and loading them with the host's native tool or an explicit read, without claiming loading from selection alone.

#### Scenario: Skills preserve workflow authority

- **WHEN** a selected skill's instructions suggest actions outside the role's permissions or workflow boundaries
- **THEN** role instructions retain their write limits, independent review, finding rules, and human approval requirements.
