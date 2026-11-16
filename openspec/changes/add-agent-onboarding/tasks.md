# Tasks: add-agent-onboarding

Boxes record implementation with observed validation. They do not imply formal stage review
or human approval. Actual commands and remaining runtime limits are recorded in tests.md.

## 1. Local onboarding evidence

- [x] 1.1 Implement Local agent diagnostics and Honest hook evidence for both hosts using readonly supported layouts and actionable recovery. Size: M. Depends on: none. Check: missing, enabled, disabled, unknown, and no-execution fixtures; test. Source: agent-onboarding, Local agent diagnostics and Honest hook evidence.
- [x] 1.2 Add Onboarding command integration to shared metadata, help, host preferences, completion, and JSON reporting. Size: S. Depends on: 1.1. Check: offline help, precedence, usage, JSON, and generated-reference tests. Source: agent-onboarding, Onboarding command integration.

## 2. Start a coordinated task

- [x] 2.1 Implement Safe task preparation with task-skill prompt, host argv, diagnostics, and side-effect-free print/JSON modes. Size: M. Depends on: 1.2. Check: literal requests, missing setup, and no task/receipt creation. Source: agent-onboarding, Safe task preparation.
- [x] 2.2 Implement Explicit interactive host launch after terminal checks and local preflight. Size: M. Depends on: 2.1. Check: fake host invocation, refusal, literal argv, and exit propagation. Source: agent-onboarding, Explicit interactive host launch.
- [x] 2.3 Implement Workflow-first session context and Safe missing-CLI bootstrap within existing limits while preserving conditional gate evidence facts. Size: S. Depends on: none. Check: configured and unconfigured repositories, host notices, missing CLI/PATH, and context size tests. Source: agent-onboarding, Workflow-first session context and Safe missing-CLI bootstrap.

## 3. Documentation and validation

- [x] 3.1 Document Accurate onboarding guidance, install/check/start recovery, optional gate setup, handoff-only use, and host trust limits; regenerate the command reference. Size: M. Depends on: 1.2, 2.2, 2.3. Check: repository documentation, generated files, and scenario mappings. Source: agent-onboarding, Accurate onboarding guidance; design.md.
- [x] 3.2 Run required focused and repository checks and record actual results and remaining host-runtime limits. Size: M. Depends on: 3.1. Check: commands and observed results in tests.md; CONTRIBUTING.md. Source: proposal.md, Validation; CONTRIBUTING.md.
