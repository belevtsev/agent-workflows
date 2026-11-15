# Tasks: add-configurable-advisers

These boxes record implemented behavior and completed validation. They imply no formal stage
review or human approval; those remain unrecorded in `reviews.md`.

## 1. Configuration and execution

- [x] 1.1 Implement Optional adviser configuration and Explicit role access, with strict merged policy and empty defaults. Size: M. Depends on: none. Check: configuration and access scenarios; test. Source: `configurable-advisers`: Optional adviser configuration, Explicit role access.
- [x] 1.2 Implement Advice and decisions and Direct bounded execution using explicit context, direct argv, process bounds, and no retries. Size: M. Depends on: 1.1. Check: request and subprocess scenarios; test. Source: `configurable-advisers`: Advice and decisions, Direct bounded execution.

## 2. Shared records and checkpoints

- [x] 2.1 Implement Portable task bindings and Immutable consultation history with explicit refresh, failed attempts, and appended dispositions. Size: M. Depends on: 1.2. Check: binding and history scenarios; test. Source: `configurable-advisers`: Portable task bindings, Immutable consultation history.
- [x] 2.2 Implement Required checkpoint receipts using existing artifact content ids and stage reporting. Size: M. Depends on: 2.1. Check: freshness and offline-check scenarios; test. Source: `configurable-advisers`: Required checkpoint receipts.
- [x] 2.3 Implement Small task promotion without losing history or accepting two locations. Size: S. Depends on: 2.1. Check: promotion scenarios; test. Source: `configurable-advisers`: Small task promotion.

## 3. Host workflow and reporting

- [x] 3.1 Expose the adviser commands and root consultation command, including help and JSON output. Size: M. Depends on: 2.2, 2.3. Check: fake-CLI end-to-end scenarios; test. Source: `configurable-advisers`: Advice and decisions, Required checkpoint receipts.
- [x] 3.2 Update Coordinator brokering in every role text and regenerate host files, retaining review independence and human approvals. Size: S. Depends on: 3.1. Check: role generation and unchanged text budgets; review of handoff instructions. Source: `configurable-advisers`: Coordinator brokering.
- [x] 3.3 Add Usage disclosure without inventing actual model identity or measured adviser tokens. Size: S. Depends on: 2.1. Check: token report lists consultations as unmeasured while measured totals stay intact; test. Source: `configurable-advisers`: Usage disclosure.
- [x] 3.4 Add Role selection table and Differentiated model routing for both hosts, with repository overrides and explicit developer precedence. Size: S. Depends on: 1.1. Check: roles output and model-routing scenarios; test. Source: `configurable-advisers`: Role selection table, Differentiated model routing.

## 4. Documentation and validation

- [x] 4.1 Document configuration, commands, workflows, checkpoints, limits, model recommendations, and unmeasured usage; map scenarios to tests without claiming live model evaluation. Size: M. Depends on: 3.2, 3.3, 3.4. Check: change-record checks and repository documentation checks. Source: every requirement in `configurable-advisers`; design.md.
- [x] 4.2 Run build, vet, race tests, lint, vulnerability checks, generated-file checks, and change-record checks; record actual results and remaining limits. Size: M. Depends on: 4.1. Check: named commands exit successfully or limitations are reported explicitly. Source: proposal.md, Validation and limits; CONTRIBUTING.md.
