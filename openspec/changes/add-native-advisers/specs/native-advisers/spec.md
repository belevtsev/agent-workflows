# Native advisers

## ADDED Requirements

### Requirement: Built-in provider configuration

Kitsu SHALL provide TypeSafe Jev and SubQ Snap presets plus a configurable System One-compatible
provider through version 2 definitions. Credentials SHALL be referenced by environment variable.
No adviser SHALL gain role access from credential presence. Version 1 CLI definitions and
whole-definition repository precedence SHALL remain supported.

Source: owner's accepted built-in adviser plan.

#### Scenario: Explicit native definitions coexist with legacy CLIs

- **WHEN** a version 2 repository defines native presets beside a CLI and replaces a personal definition
- **THEN** preset defaults resolve, replacement is complete, and the CLI retains its earlier fingerprint.

### Requirement: Typed single-question consultation

Native advisers SHALL accept one decision, score, or boolean request per call. Score and boolean
requests SHALL use version 2. Choices, probabilities, confidence, and fractional scores SHALL
remain typed; zero SHALL remain distinct from missing. Unsupported kinds and invalid input SHALL
be refused before recording or contacting a provider.

Source: owner's selected capabilities and single-question preference.

#### Scenario: Typed results preserve uncertainty

- **WHEN** each supported question type receives a valid provider response
- **THEN** its typed answer, zero values, probabilities, and provider-reported identity and usage are retained.

#### Scenario: Invalid preparation makes no attempt

- **WHEN** credentials are missing, a request kind is unsupported, or the definition differs from the binding
- **THEN** no provider call or pending consultation is created.

### Requirement: Bounded provider execution

Native execution SHALL perform one HTTPS attempt with bounded bodies, timeout, and cancellation.
It SHALL refuse redirects and never retry or switch providers automatically. Raw HTTP error
bodies and credentials SHALL not enter execution records. Invalid typed responses SHALL fail.

Source: accepted plan; existing adviser single-attempt contract.

#### Scenario: HTTP failures do not retry or reveal credentials

- **WHEN** a provider rejects authentication, credits, access, limits, or input, or returns malformed/oversized output
- **THEN** one safe failed outcome remains, without credentials or its raw error body.

#### Scenario: Redirects and stopped calls remain bounded

- **WHEN** a response redirects, the deadline expires, or the caller cancels
- **THEN** no redirect is followed and the corresponding failed, timed-out, or cancelled result is retained.

### Requirement: Portable compatible history

Native bindings and consultations SHALL use version 2 while old histories remain readable.
New native fingerprints SHALL include effective nonsecret settings but not credential values.
Typed results SHALL be validated against their saved request and provider response offline.
Current-content checkpoints SHALL still require successful addressed consultations.

Source: accepted plan; existing shared-history and checkpoint contracts.

#### Scenario: Typed history is immutable and readable offline

- **WHEN** local credentials and definitions are unavailable or a normalized result is altered
- **THEN** intact history remains inspectable offline and altered results fail validation.

#### Scenario: A native receipt needs disposition and fresh content

- **WHEN** a successful native consultation is addressed and later its consulted content changes
- **THEN** it satisfies the checkpoint only before that content change.

#### Scenario: Failed and stale calls retain answers without satisfying checkpoints

- **WHEN** the provider fails or content changes during execution
- **THEN** the attempt remains recorded and satisfies no checkpoint, preserving any valid returned answer and usage.

### Requirement: Separate adviser usage

Kitsu SHALL report native provider token counts separately from host totals. Missing or CLI usage
SHALL remain unknown and make adviser totals incomplete. Requested model identity SHALL not be
treated as a verified actual model or proof of usage.

Source: accepted plan; provider response usage contracts.

#### Scenario: Mixed CLI and API usage is partial

- **WHEN** a task has a native call with reported usage and a CLI call without it
- **THEN** native counts are reported separately, completeness is false, and host totals are unchanged.

### Requirement: Discoverable adviser capabilities

Kitsu SHALL expose an offline provider catalog and supported request kinds in configured/bound
adviser listings. Documentation and role instructions SHALL explain native setup and typed answers
while preserving coordinator ownership, independent review, and human approval.

Source: owner's built-in adviser request and accepted explicit-activation preference.

#### Scenario: Provider discovery needs no repository or credentials

- **WHEN** the user runs advisers providers outside a repository
- **THEN** text or JSON lists presets, model defaults, credential variable names, and supported kinds without network calls.

#### Scenario: Host instructions explain native judgments

- **WHEN** Kitsu supplies role and coordinator instructions
- **THEN** they describe supported kinds and probabilities without granting automatic approval, and generated host files match.
