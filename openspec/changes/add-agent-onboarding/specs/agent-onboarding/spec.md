# agent-onboarding

## ADDED Requirements

### Requirement: Local agent diagnostics

Kitsu SHALL provide kitsu agent check for codex and claude. It MUST inspect local Kitsu PATH
availability, the selected host CLI, installed and enabled Kitsu plugin, task skill, and hook
bundle configuration, with component evidence and actionable recovery. It MUST NOT require
kitsu.toml, a Git gate, authentication, or model access to inspect onboarding.

Source: owner's request to verify CLI/plugin/hooks before starting a coordinated task.

#### Scenario: Supported local setup is inspectable

- **WHEN** a supported host has locally available CLI, enabled Kitsu plugin, task skill, and hook bundle
- **THEN** the report names the available components and their evidence without requiring the supporting Git gate.

#### Scenario: Missing or disabled components give next steps

- **WHEN** a required local component is missing or an inspected setting explicitly disables it
- **THEN** the diagnostic names the problem and a relevant install, enable, PATH, or host-setting recovery step.

#### Scenario: Diagnostics run without execution or writes

- **WHEN** agent check inspects local setup in text or JSON mode
- **THEN** it executes no host, model, hook, installer, authentication flow, or config write.

### Requirement: Honest hook evidence

Diagnostics MUST distinguish configured hooks, demonstrated disable settings, and unknown
trust or session execution. File presence MUST NOT imply trusted hooks, runtime verification,
or successful skill loading. Unsupported or inconclusive installation evidence MUST state
what cannot be established; it MAY block local preflight but MUST NOT assert actual absence.
Unknown hook trust or runtime execution alone MUST NOT block local readiness.
Codex guidance MUST direct the user to review and trust the current exact hooks through /hooks.

Source: official host plugin/hook guidance and the accepted evidence boundary.

#### Scenario: Configured hooks retain unknown runtime evidence

- **WHEN** a local plugin provides hook configuration without direct session execution evidence
- **THEN** the report identifies configuration separately from unknown trust and runtime behavior.

#### Scenario: Unsupported local evidence stays unknown

- **WHEN** an inspected host layout cannot establish installation, enablement, or hook policy reliably
- **THEN** the report states what cannot be established and gives a host-native inspection step without claiming actual absence or runtime verification.

### Requirement: Onboarding command integration

The new agent group and task start leaf SHALL use shared command metadata for dispatch, help,
completion, and generated documentation. Host selection MUST follow flag, nonempty KITSU_HOST,
private worktree preference, then personal preference. Help MUST work before configuration,
required operands, terminals, or diagnostics, and invalid usage MUST retain exit 2 and targeted help.

Source: accepted command surface and existing CLI UX contracts.

#### Scenario: Onboarding help works before setup

- **WHEN** a new command's help is requested with no repository or invalid configuration
- **THEN** it exits 0 with complete usage without diagnosis, launch, writes, or prompting.

#### Scenario: Onboarding follows the effective host preference

- **WHEN** explicit, environment, worktree, or personal host settings are supplied
- **THEN** the highest-precedence selected value controls diagnostics and handoff, and invalid selected values fail clearly.

### Requirement: Safe task preparation

kitsu task start SHALL accept a coding request and prepare an explicit Kitsu task-skill prompt
for the selected host. Mutually exclusive --print and --json modes MUST return that handoff
and diagnostics without executing a host or requiring its CLI to be installed. Preparation
MUST NOT create a task folder, receipt, workflow stage, approval, or usage record.

Source: owner's request for a clear start path that also works in desktop and incomplete setups.

#### Scenario: Handoff-only modes remain usable with broken setup

- **WHEN** task start receives --print or --json while local host setup is incomplete
- **THEN** it returns the prepared task-skill handoff with diagnostics without execution or workflow record creation.

#### Scenario: Requests remain literal task text

- **WHEN** a request contains spaces, Unicode, option-like text, or shell metacharacters
- **THEN** preparation preserves that request as prompt data without evaluating shell content.

### Requirement: Explicit interactive host launch

Default task start SHALL explicitly launch the selected interactive host only with required
terminals and acceptable local preflight. It MUST pass the handoff as process arguments without
a shell and retain host execution, permission, authentication, model, and delegation authority.
Known local failures MUST prevent launch; unknown trust/runtime MUST be disclosed without
being presented as verified. Launch MUST NOT claim that a workflow stage has completed.

Source: accepted explicit launch decision, host CLI interface, and existing workflow authority.

#### Scenario: Ready local preflight launches the selected host

- **WHEN** default task start has required terminals and acceptable local setup
- **THEN** it invokes only the chosen interactive host with the task-skill prompt and preserves the host's result.

#### Scenario: Missing terminal or broken preflight prevents launch

- **WHEN** default task start lacks required terminals or has a demonstrated local setup failure
- **THEN** it explains the correction and handoff-only alternative without invoking the host or creating workflow records.

### Requirement: Workflow-first session context

Session-start context in a Git repository SHALL introduce Kitsu's task skill and host
orchestration before optional gate facts, including repositories without kitsu.toml. It MUST
not start a workflow automatically. Existing gate installation, observe-mode, and check-evidence
facts MUST stay accurate and the context MUST remain within its existing 1,000-character budget.

This replaces the historical Session context rule in reduce-cw-to-evidence-gate/agent-integration
that a Git repository without kitsu.toml received no context. The workflow introduction MUST
precede the first gate paragraph; that paragraph MUST disclose absent hooks before describing
what installed hooks would enforce. Outside Git, context MUST remain silent.

Source: owner's onboarding request; supersedes reduce-cw-to-evidence-gate, agent-integration,
Session context; existing concise context/evidence contracts.

#### Scenario: An unconfigured repository gets workflow guidance

- **WHEN** session context runs in a Git repository without kitsu.toml
- **THEN** it describes how to start Kitsu coordination without claiming installed Git hooks or automatically starting work.

#### Scenario: Configured context preserves accurate gate facts

- **WHEN** session context runs with declared checks and any supported gate installation mode
- **THEN** it introduces the task workflow and accurately describes gate state and evidence commands within the context budget.

### Requirement: Safe missing-CLI bootstrap

When a SessionStart hook cannot find Kitsu on usable absolute PATH entries, it SHALL print
install/PATH guidance without requiring kitsu.toml, Git, or a repository-root working directory.
It MUST NOT claim gate enforcement or copy the workflow body. Existing absolute-PATH-only
lookup and successful nonblocking exit MUST remain; POSIX hooks with no usable absolute PATH
entry MUST execute no repository binary and may remain silent.

This replaces the historical Instruction text comes from kitsu subrule that missing-CLI
guidance required a declaration and said commits would be denied.

Source: owner's onboarding request; supersedes reduce-cw-to-evidence-gate, agent-integration,
Instruction text comes from kitsu, missing-CLI guidance; existing safe host-hook lookup.

#### Scenario: Missing CLI gives guidance before repository setup

- **WHEN** a SessionStart hook has usable absolute lookup paths but no installed Kitsu in an undeclared directory or subfolder
- **THEN** it prints install/PATH guidance without claiming enforced Git hooks or starting installation.

#### Scenario: Bootstrap never executes a repository binary

- **WHEN** PATH begins with relative entries or contains no usable absolute entry and the repository holds an executable named kitsu
- **THEN** the hook does not run that file and exits successfully, remaining silent when no safe POSIX lookup path exists.

### Requirement: Accurate onboarding guidance

Documentation SHALL explain install, check, start, manual skill invocation, handoff-only modes,
diagnostic evidence and exit behavior, host trust recovery, and the optional supporting Git gate.
Generated command documentation MUST match metadata. Validation claims MUST describe actual
observations and preserve existing role, review, approval, and gate authority.

Source: accepted practical onboarding documentation and repository contribution requirements.

#### Scenario: Onboarding docs distinguish setup from execution

- **WHEN** a reader follows install/check/start guidance or uses a desktop handoff
- **THEN** the docs identify local setup evidence, remaining trust/runtime checks, and the action that actually starts host work.
