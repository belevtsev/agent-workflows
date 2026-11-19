# Configured task workflow

## ADDED Requirements

### Requirement: Install and initialize role defaults

An interactive installer SHALL offer Codex, Claude Code and CLI-only options. An unattended installer SHALL accept an explicit host. A successful matching plugin setup SHALL persist the selected host as the personal default. CLI-only and failed host setup SHALL leave the saved selection intact.

When the Git worktree lacks a roles file, an explicit configured daily run SHALL use the saved host for required specialists. `kitsu role init` SHALL create a new version 2 file from that preference or an explicit host. A developer or plugin session SHALL be able to change an individual repository role with the validated role CLI.

Source: Developer request to choose install defaults and configure each role from either plugin, TOML or the CLI.

#### Scenario: Install a matched host and save its default

- **WHEN** a host install registers the matching plugin
- **THEN** the installer saves that host as the default

#### Scenario: Initialize role assignments from the saved default

- **WHEN** the developer initializes roles in a worktree without a roles file
- **THEN** the new policy uses the saved default host for every required role and a single role edit changes only its specified assignment

#### Scenario: Allow validated role settings from either plugin

- **WHEN** a plugin session invokes a direct repository role assignment command
- **THEN** the guard lets the role CLI validate and apply it, while retaining approval for global settings and other policy changes

### Requirement: Assign and retain specialist hosts

A version 2 repository policy SHALL assign each required specialist to a supported native host and resolve its requested model, effort and skills. A started task SHALL retain those assignments across resume.

Source: Developer request for one configurable role workflow across Codex and Claude Code.

#### Scenario: Resume with the pinned role assignment

- **WHEN** a configured task starts an assigned role and a later preparation sees different role settings
- **THEN** the saved task keeps its original assignment and a conflicting managed preparation stops

### Requirement: Verify a scoped daily candidate

Daily execution SHALL record work in the current task ledger, protect the agreed test and code boundaries, and require current executable and review evidence before authorized delivery.

The Go verifier SHALL run an assertion only when its exact arguments match a trusted declaration or the developer approved those arguments for the task.

Source: Developer request for an architecture, test writer, implementation, verification and reviewer flow.

#### Scenario: Verify the exact candidate without changing the assertions

- **WHEN** the worktree, staged content or tests change during verification
- **THEN** the old verification cannot satisfy the changed candidate

#### Scenario: A worker proposes an unapproved assertion

- **WHEN** a test writer returns arguments outside the task's approval and trusted check declaration
- **THEN** the verifier shows the proposed command and waits for approval before running any baseline or final assertion with those arguments

### Requirement: Execute a managed assignment under its saved approvals

Managed execution SHALL use the selected role and original saved dispatch. A native handback SHALL remain subject to the current OpenSpec stage, write bounds, checks and developer approval. A configured reviewer handback SHALL match native start and stop notices from its reserved parent and a distinct reviewer on the pinned candidate.

Source: Developer request to use configured specialists in the existing OpenSpec approval workflow.

#### Scenario: Managed design waits for specification approval

- **WHEN** a configured full task requests its design stage before specification approval
- **THEN** Kitsu refuses to prepare the stage and no worker starts

#### Scenario: Reuse a completed managed worker handback

- **WHEN** the same configured dispatch is run again after its worker returns
- **THEN** Kitsu returns the saved handback identity without another native launch

#### Scenario: A configured review parent returns without a native reviewer

- **WHEN** a reserved review parent finishes without matching native reviewer start and stop notices
- **THEN** Kitsu keeps the review unfinished

### Requirement: Commit only an explicitly scoped managed candidate

A configured Git delivery SHALL match the authorized paths, verified staged tree and checkout, and execute the installed Git gate.

Source: Developer request to protect scoped delivery and keep unrelated changes.

#### Scenario: Refuse a managed commit that omits a changed path

- **WHEN** the staged candidate changes a path outside the explicit include list
- **THEN** Kitsu leaves the branch and staging intact and identifies the missing inclusion
