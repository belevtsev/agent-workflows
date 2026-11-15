# Configurable advisers

## ADDED Requirements

### Requirement: Optional adviser configuration

Kitsu SHALL load optional personal definitions from `os.UserConfigDir()/kitsu/advisers.toml` and
repository definitions from `kitsu-advisers.toml`, using strict version 1 TOML. A repository
definition MUST replace the entire personal definition of the same name. A definition MUST name
a nonempty model, a direct argument array with exactly one literal `{model}` argument, an optional
`{answer_file}` argument, and a timeout from 1s to 1h, default 5m. Personal files MUST contain no
access policy or checkpoints.

Source: accepted adviser configuration plan; owner's configurable-adviser request.

#### Scenario: No adviser files exist

- **WHEN** neither configuration file exists
- **THEN** the adviser list is empty and existing workflows remain usable without adviser execution.

#### Scenario: Repository definitions replace personal definitions

- **WHEN** both files define the same adviser with different model, command, and timeout
- **THEN** all three fields come from the repository definition, with omitted timeout taking the default.

#### Scenario: Invalid adviser configuration is refused

- **WHEN** configuration has an unknown field, invalid reference, invalid placeholder, or timeout outside the bounds
- **THEN** the command reports the invalid configuration before starting an adviser.

### Requirement: Explicit role access

Repository policy SHALL own access for the coordinator and seven known roles. An explicit role
list MUST replace `[access].default`, including an empty list; absent role and default lists MUST
deny access. Checkpoints MUST reference defined advisers allowed for their requester.

Source: accepted repository policy and per-role access plan.

#### Scenario: Role access replaces the default

- **WHEN** default permits an adviser but the requester's explicit list is empty
- **THEN** that requester cannot execute the adviser.

#### Scenario: Inaccessible required adviser is refused

- **WHEN** a checkpoint names an adviser outside its role's allowed list
- **THEN** configuration is rejected before binding or execution.

### Requirement: Advice and decisions

`kitsu consult` SHALL accept one strict UTF-8 version 1 JSON document from `--request <file|->`
with kind `advice` or `decision`, a nonempty question, and optional explicit repository-relative
context files. An advice request MUST have no choices; a decision MUST have at least two choices
with distinct nonempty ids and text. Request and rendered prompt MUST each fit in 1 MiB. Context
MUST be regular UTF-8 files within the repository, including through symlinks.

Source: owner's questions-and-choices request; accepted request schema and explicit context plan.

#### Scenario: A permitted role compares named choices

- **WHEN** the coordinator submits a decision request for an allowed adviser with two choices and explicit files
- **THEN** the rendered prompt contains that question, both named choices, and only the listed files as context.

#### Scenario: Invalid requests execute nothing

- **WHEN** a request contains unknown or duplicate keys, too few choices, an escaping path, or oversized input
- **THEN** Kitsu rejects it before starting the configured command.

### Requirement: Direct bounded execution

Kitsu SHALL execute exactly one configured CLI command directly, supplying the requested model
and prompt on stdin with a bounded timeout, cancellation, output, and process cleanup. The answer
MUST be nonempty UTF-8 within 1 MiB. A requested answer file MUST be the sole answer source.
Kitsu MUST use existing CLI authentication without storing API keys, and MUST NOT retry or
substitute an adviser automatically. Actual model and token usage MUST remain unknown.

Source: accepted local CLI execution plan; existing `internal/proc` process contract.

#### Scenario: CLI returns an answer

- **WHEN** the selected local command exits successfully with a bounded nonempty answer
- **THEN** Kitsu records success, the exact answer, requested identity, and unknown actual model and usage.

#### Scenario: Failed calls remain unsuccessful

- **WHEN** the command fails, times out, is cancelled, returns an empty answer, or exceeds its output bound
- **THEN** its attempt is retained with that outcome and satisfies no checkpoint, with no automatic retry.

### Requirement: Portable task bindings

A binding SHALL snapshot nonsecret adviser model names and definition fingerprints, access, and
checkpoints for one task and workflow. Offline readers MUST use the binding without personal
configuration or an installed provider CLI. Configuration changes MUST require explicit
`--refresh`, preserving earlier revisions. Execution MUST refuse a current local definition that
differs from its bound identity.

Source: accepted snapshot, explicit refresh, and offline CI plan.

#### Scenario: Shared binding works offline

- **WHEN** another checkout has the binding and records but no personal adviser file or provider executable
- **THEN** list, status, and structural checks inspect the saved policy without executing a provider.

#### Scenario: Configuration changes need refresh

- **WHEN** the current adviser command or model differs from the active binding
- **THEN** execution refuses the mismatch until an explicit refresh creates a new revision without deleting history.

### Requirement: Immutable consultation history

Kitsu SHALL preserve each consultation's request, exact rendered prompt, answer, identity,
checkpoint, content id, and execution metadata without rewriting the completed attempt. A
disposition MUST append `accept`, `reject`, or `defer` with a nonempty rationale. An optional
recorded choice MUST name a choice from its request; free-form answers MUST imply no selection.
Shared history MUST omit command arguments,
environment variables, keys, and raw diagnostics.

Source: owner's durable adviser answers and decisions request; accepted shared record plan.

#### Scenario: Dispositions append to the original answer

- **WHEN** a coordinator rejects or defers an answer with a reason and later records another disposition
- **THEN** the original consultation and each disposition remain inspectable without changing the answer.

#### Scenario: Recorded decision choices match offered choices

- **WHEN** a disposition names a choice outside the request
- **THEN** resolution fails without adding an invalid disposition.

### Requirement: Required checkpoint receipts

Repository checkpoints SHALL apply only to `spec`, `design`, or `review`. A satisfied receipt
MUST match the active binding, checkpoint id, requester, adviser identity, stage, and artifact
content id, and have a successful answer plus an addressed disposition with a reason. Every
required adviser MUST have a receipt. `change status` MUST hold the corresponding stage for a
missing or stale receipt; `change check` MUST validate structure without requiring future advice.
It MUST discover active small-task histories and accept a named small history without a full
change folder, validating saved record structure and references on both workflow paths.
`advisers check` MUST compare current artifacts, while completed status may use the committed
task baseline. Dispositions MUST NOT waive serious findings, reviews, or human approvals.

Source: owner's required-stage-advice request; accepted freshness and stage integration plan.

#### Scenario: Fresh addressed advice satisfies a checkpoint

- **WHEN** every named adviser has a successful matching receipt on current content and a reasoned disposition
- **THEN** the checkpoint check passes without recording a review or approval.

#### Scenario: Changed content requires fresh advice

- **WHEN** a required stage's artifact content changes after its consultation
- **THEN** the checkpoint becomes stale and holds the stage until matching advice is addressed.

#### Scenario: Future missing advice is not malformed structure

- **WHEN** a task has valid records but has not reached a later required checkpoint
- **THEN** change check passes structural validation while stage status still requires that checkpoint when due.

#### Scenario: Small task history is structurally checked

- **WHEN** a small task has saved adviser history and change check runs globally or names that task
- **THEN** it checks that history without requiring a full change folder or future checkpoint answers, and refuses malformed, conflicting, invalid, or symlink record entries.

### Requirement: Small task promotion

Full tasks SHALL keep adviser history under `openspec/changes/<task>/advisers/`, and small tasks
under `openspec/changes/.advice/<task>/`. Promotion MUST require explicit full-workflow refresh,
an absent destination adviser folder, and movement of the complete history. Two histories for
one task MUST be refused.

Source: accepted small-workflow history and promotion plan.

#### Scenario: Small history moves into a full task

- **WHEN** a small task is promoted with `--workflow full --refresh` and an absent adviser destination
- **THEN** the full record contains all prior binding revisions, consultations, and dispositions and the old location is absent.

#### Scenario: Competing histories are refused

- **WHEN** both full and small adviser locations exist for one task
- **THEN** commands report the ambiguity instead of choosing or merging a history.

### Requirement: Coordinator brokering

Every role's instructions SHALL return adviser requests to the coordinator and forbid direct
execution or adviser-record writes. The coordinator MUST check role writes before executing and
return the answer before the requester resumes. A reviewer MUST form its own assessment first and
receive no author's consultations as guidance. Advice MUST replace no review, approval, or
existing finding rule. Generated files and prompt budgets MUST remain checked.

Source: accepted role ownership, review independence, and existing human approval rules.

#### Scenario: Role asks through the coordinator

- **WHEN** a role needs advice from its brief's permitted list
- **THEN** its instructions require returning the question, choices, and sources to the coordinator for execution after the write check.

#### Scenario: Reviewer remains independent

- **WHEN** a reviewer receives a work product that its author discussed with an adviser
- **THEN** its instructions require its own assessment before consulting and prohibit the author's consultations as review guidance.

### Requirement: Usage disclosure

Token reporting SHALL list adviser consultations separately as unmeasured usage and MUST NOT add
invented usage to measured host totals or present the requested model as a verified actual model.

Source: existing unknown-is-not-zero token contract; accepted unmeasured adviser usage plan.

#### Scenario: Adviser call has no measured usage

- **WHEN** a task has measured host usage and a saved adviser consultation
- **THEN** the host totals stay unchanged and the adviser is listed separately with unknown actual model and token usage.

### Requirement: Role selection table

`kitsu roles --host claude|codex` SHALL show role, model, reasoning effort, and allowed advisers,
including the coordinator as the host session rather than inventing a configured model.
`--task` MUST use saved adviser access offline; `--json` MUST retain machine-readable values.
Without `--host`, both hosts MUST remain visible.

Source: owner's request for CLI and documentation role/model/reasoning/adviser tables.

#### Scenario: Host selection shows model reasoning and access

- **WHEN** a developer selects a supported host and optionally a bound task
- **THEN** the table shows each role's selected model, reasoning effort, and appropriate adviser access, with the coordinator identified as the host session.

### Requirement: Differentiated model routing

Default role models and efforts SHALL match the route in the design for both hosts, with Codex
architect on `gpt-6.1-sol` at `max`. Repository overrides and the developer's explicit model
instructions MUST take precedence. Kitsu MUST NOT escalate automatically or claim benchmarked
quality from these recommendations.

Source: owner's host-specific route request and explicit Sol/max architect correction.

#### Scenario: Roles use their configured routing

- **WHEN** roles are listed with and without repository overrides
- **THEN** the defaults match the documented route and each supplied override replaces its field without changing other roles.
