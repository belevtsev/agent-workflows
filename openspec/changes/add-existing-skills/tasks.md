# Tasks: add-existing-skills

Boxes track implementation and validation evidence only. They imply no independent stage
review or human approval; those remain unrecorded in `reviews.md`.

## 1. Inventory and bindings

- [x] 1.1 Implement Original local skill inventory with supported host layouts, explicit roots, and preserved source directories. Size: M. Depends on: none. Check: local discovery scenarios and no-process tests; test. Source: `existing-skills`: Original local skill inventory.
- [x] 1.2 Implement Validated skill identities and Observable host policy, including ambiguity, unavailable entries, and manual-only refusal. Size: M. Depends on: 1.1. Check: metadata, duplicate-name, disable, and plugin-policy scenarios; test. Source: `existing-skills`: Validated skill identities, Observable host policy.
- [x] 1.3 Implement Layered role bindings with optional personal lists, repository replacement, and explicit empty arrays. Size: M. Depends on: 1.2. Check: configuration precedence and strict scope scenarios; test. Source: `existing-skills`: Layered role bindings.
- [x] 1.4 Implement Explicit selection editing and Role selection reporting while preserving model and reasoning controls. Size: M. Depends on: 1.3. Check: set, clear, reset, global, coordinator, table, and JSON scenarios; test. Source: `existing-skills`: Explicit selection editing, Role selection reporting.

## 2. Resolution and shared history

- [x] 2.1 Implement Original source resolution for a fresh worker brief without loading or executing skills. Size: M. Depends on: 1.4. Check: path, hash, source-change, and read-only resolution scenarios; test. Source: `existing-skills`: Original source resolution.
- [x] 2.2 Implement Portable immutable selections and Offline history checks on both workflows with safe history promotion. Size: M. Depends on: 2.1. Check: receipt portability, immutability, promotion, offline, malformed, and symlink scenarios; test. Source: `existing-skills`: Portable immutable selections, Offline history checks.

## 3. Host instructions and documentation

- [x] 3.1 Implement Existing skill handoff in generic role instructions and generated Claude tools while retaining the existing safety and workflow boundaries. Size: M. Depends on: 2.2. Check: generated-file and prompt-budget tests; review of handoff instructions. Source: `existing-skills`: Existing skill handoff.
- [x] 3.2 Document discovery, per-role binding, personal precedence, clear/reset, duplicate resolution, source handoff, receipts, limitations, and illustrative language expertise. Size: M. Depends on: 3.1. Check: repository docs, CLI examples, and change-record mapping. Source: every requirement in `existing-skills`; design.md.
- [x] 3.3 Run required build, vet, race, lint, vulnerability, generated-file, documentation, and record checks; record only observed outcomes. Size: M. Depends on: 3.2. Check: actual commands and limitations in tests.md. Source: proposal.md, Validation and limits; CONTRIBUTING.md.
