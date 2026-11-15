# cli-ux

## ADDED Requirements

### Requirement: Preferred command tree

Kitsu SHALL expose the accepted singular task, role, skill, adviser, check, gate, usage, config,
and completion groups, plus version and advanced context/hook/guard commands. Existing commands
MUST remain quiet aliases with preserved execution and bare-command behavior. Root and
preferred groups with no arguments MUST print help and exit 0.

Source: owner's accepted CLI UX surface and compatibility plan.

#### Scenario: Preferred and existing commands share behavior

- **WHEN** a supported action is invoked through a preferred path or retained alias
- **THEN** it preserves the same underlying behavior without a deprecation banner or implicit editor.

#### Scenario: Empty preferred commands show help

- **WHEN** Kitsu or a preferred group is invoked without a subcommand
- **THEN** help is printed and exits 0 while legacy bare-command behavior remains preserved.

### Requirement: Shared command metadata

Dispatch, help, examples, generated command documentation, completion, and preferred command guard classification SHALL derive
from shared command metadata. The implementation MUST NOT maintain conflicting command trees
or let a preferred alias bypass existing policy or gate guards.

Source: accepted ux_types.go/ux_registry.go command authority and guard parity design.

#### Scenario: Metadata reaches every command consumer

- **WHEN** the preferred registry contains a command, operand, or option
- **THEN** dispatch, help, examples, generated command documentation, and completion agree on that interface and mutations keep their guard classification.

### Requirement: Complete safe help

Every root, group, leaf, and supported alias SHALL accept `-h`, `--help`, `help PATH`, and
`GROUP help SUBPATH` where applicable. Help MUST run before required-operand, Git, configuration,
host preference, provider, or TTY validation, and MUST perform no writes, execution, or UI
interaction. Arguments following the check-program `--` boundary MUST remain program arguments.

Source: owner's complete help and no-side-effect requirements.

#### Scenario: Help works before repository and configuration setup

- **WHEN** help is requested outside Git with invalid configuration or missing required operands
- **THEN** the requested help exits 0 without reading workflow state, writing files, executing actions, or opening a form.

#### Scenario: Every supported help spelling reaches the leaf

- **WHEN** a leaf or alias is requested through a supported help ingress
- **THEN** its usage, operands, options, defaults, and examples describe that exact command.

#### Scenario: Program help flags stay beyond the separator

- **WHEN** check add receives `--help` after its program separator
- **THEN** the flag stays with the declared program instead of requesting Kitsu help.

### Requirement: Concise usage errors

Invalid command usage SHALL exit 2 with a concise error, relevant leaf usage, and the exact help
command. Typo suggestions MUST be informational and MUST NOT execute a corrected command,
mutate configuration, or prompt.

Source: accepted leaf error and safe suggestion behavior.

#### Scenario: Invalid leaf arguments give targeted guidance

- **WHEN** a known leaf has a missing operand, invalid flag, or invalid option value
- **THEN** it exits 2 with that leaf's usage and exact help guidance without executing the action.

#### Scenario: A typo only suggests a correction

- **WHEN** a command or subcommand is misspelled
- **THEN** any suggested correction is printed without execution, mutation, or interactive fallback.

### Requirement: Readonly inspection

Task, role, skill, adviser, and check list/show actions SHALL inspect existing state without
provider calls or implicit setup. Full task folders and small histories MUST be discoverable.
Small histories MUST keep their stage unknown rather than imply tests, reviews, or approvals.
JSON options MUST retain machine-readable information without UI or styling.

Source: accepted inspect/list actions and honest small-task history reporting.

#### Scenario: Existing state can be inspected without execution

- **WHEN** a supported list or show action reads local configuration or saved records
- **THEN** it reports existing facts and JSON where supported without provider calls, setup writes, or prompts.

#### Scenario: Small histories do not invent workflow stages

- **WHEN** task list or show encounters a saved small history without a full task record
- **THEN** it reports the available history and an unknown stage without inventing completion or approval evidence.

### Requirement: Private host preferences

Preferred commands SHALL resolve host from explicit flag, nonempty KITSU_HOST, private worktree
GitDir/kitsu-cli.toml, then personal UserConfigDir/kitsu/config.toml. Only codex and claude MUST
be accepted, with origins inspectable. Version 1 preference files MUST hold only supported CLI
preferences, remain private, and be worktree-isolated. Set/reset MUST default to local scope;
`--global` MUST use personal scope. Legacy ingress MUST keep existing host behavior.
Removing the supporting gate MUST preserve the private CLI host preference.

Source: owner's accepted precedence, storage, privacy, and legacy compatibility plan.

#### Scenario: Explicit host and preference origins follow precedence

- **WHEN** flag, environment, worktree, and personal values are present or removed
- **THEN** the highest-precedence configured source controls host selection and its origin, while an invalid selected value fails clearly.

#### Scenario: Local settings remain private to a worktree

- **WHEN** host preferences are set or reset locally or globally
- **THEN** only the selected private file changes and linked worktrees keep independent local values.

#### Scenario: Host preference survives gate removal

- **WHEN** the supporting gate is removed from a worktree with a saved local host preference
- **THEN** gate hooks and evidence are removed while the host preference remains available.

### Requirement: Explicit terminal editors

Only explicit role edit and config edit SHALL prompt through Huh v2. Editors MUST require
stdin/stderr TTYs, refuse JSON mode, show field help and an accessible mode, preserve draft
state while showing contextual help, preview before saving, and accept exact custom model ids
and multiple skill choices where permitted. Any nonempty NO_COLOR MUST suppress form colors
in normal and accessible modes, including with forced-color settings. Explicit Cancel MUST
exit 0 without creating configuration or lock files. Editor Ctrl+C or context cancellation
MUST exit 130 without writes, while existing command exit statuses remain unchanged.
Save MUST revalidate sources, refuse concurrent changes, and preserve existing scope guards.
Selecting a host inside role editing MUST NOT save a CLI host preference.

Source: accepted Huh v2 editor, preview, F1/accessibility, cancellation, and guarded-save plan.

#### Scenario: Noninteractive commands never open an editor

- **WHEN** a normal action lacks data or an editor lacks required terminals or requests JSON
- **THEN** it fails explicitly without prompting, writing configuration, or opening a form as fallback.

#### Scenario: Editor help and cancellation preserve the draft and files

- **WHEN** an explicit editor shows contextual help, returns to the draft, and is cancelled
- **THEN** draft choices remain available during the session and no configuration or lock file is created.

#### Scenario: Previewed saves retain scope and fresh configuration

- **WHEN** a role or config edit is saved after preview
- **THEN** validation and concurrent-change checks run before writing only permitted fields in the chosen scope.

#### Scenario: Nonempty NO_COLOR suppresses editor colors

- **WHEN** a normal or accessible editor runs with a nonempty NO_COLOR, including a forced-color environment
- **THEN** it renders without color while preserving the form flow and cancellation behavior.

#### Scenario: Editor interrupts return 130 without writes

- **WHEN** a new explicit editor is interrupted with Ctrl+C or a canceled context
- **THEN** it exits 130 without saving while explicit Cancel still exits 0 without saving.

### Requirement: Distinct skill operations

Skill attach SHALL add validated selections without replacing unrelated selections or requiring
older selected sources to remain available. Detach
MUST remove saved references even when the source is missing. Clear MUST write explicit [] to
suppress personal inheritance. Skill reset MUST remove only the skill override and restore
inheritance while preserving model/reasoning. Existing repository/global and coordinator guards
MUST remain unchanged.

Source: owner's accepted attach/detach/clear/reset distinction and retained role scope.

#### Scenario: Adding and removing skills preserve other settings

- **WHEN** a valid skill is attached or a saved reference is detached after its original source disappears
- **THEN** the requested list change preserves other role fields and unrelated selections without requiring the missing source to load.

#### Scenario: Clear and reset have different inheritance effects

- **WHEN** a role's skill selection is cleared or reset
- **THEN** clear suppresses inheritance with [] while reset restores inheritance without changing model or reasoning overrides.

### Requirement: Safe shell completion

Kitsu SHALL emit sourceable Bash, Zsh, Fish, and PowerShell completion scripts and readonly
callback suggestions from shared metadata and supported local names. Completion MUST preserve
word boundaries and values and MUST NOT evaluate user text, contact a provider/network,
execute skill/check/host/model code, mutate configuration, or open UI.

Source: accepted four-shell completion and offline callback safety plan.

#### Scenario: Each supported shell receives a completion script

- **WHEN** a supported completion shell is requested
- **THEN** Kitsu prints its script without setup writes, prompts, provider access, or command execution.

#### Scenario: Callback values remain local and correctly separated

- **WHEN** completion requests commands, flags, option values, or supported local names with quoting or separators
- **THEN** suggestions preserve the input boundaries using metadata and readonly sources without executing the words.

### Requirement: Preserved workflow authority

Preferred commands, generic role instructions, generated host files, documentation, and
diagrams SHALL use the accepted command surface without widening role writes, permissions,
experimental adviser access, review authority, or human approvals. Existing generated-file and
prompt-budget checks MUST remain. Diagram sources and actual archify SVG exports MUST stay
paired and show preferred command labels.

Source: accepted documentation and diagram updates; existing role and evidence boundaries.

#### Scenario: Workflow instructions retain existing authority

- **WHEN** preferred commands replace old references in role instructions and documentation
- **THEN** the same write limits, opt-in, independent review, approval, and gate rules remain explicit.

#### Scenario: Diagram exports match the preferred source labels

- **WHEN** command labels in affected archify sources are updated
- **THEN** their actual SVG Auto exports match those sources and are inspected in light and dark themes.
