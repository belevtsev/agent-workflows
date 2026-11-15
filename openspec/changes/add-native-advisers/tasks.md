# Tasks: add-native-advisers

Boxes record implementation and validation, not independent review or human approval.

## 1. Providers and typed calls

- [x] 1.1 Add presets, strict native configuration, and offline discovery. Size: M. Depends on: none. Check: provider and compatibility tests. Source: Built-in provider configuration; Discoverable adviser capabilities.
- [x] 1.2 Add single-question typed requests and the bounded HTTP adapter. Size: M. Depends on: 1.1. Check: local HTTPS mapping, response, failure, cancellation, and redirect tests. Source: Typed single-question consultation; Bounded provider execution.

## 2. Records and reporting

- [x] 2.1 Extend portable records and the existing consultation lifecycle. Size: M. Depends on: 1.2. Check: immutable record, preflight, freshness, disposition, and offline tests. Source: Portable compatible history.
- [x] 2.2 Report native usage separately from host tokens. Size: S. Depends on: 2.1. Check: native-only and mixed CLI/API token tests. Source: Separate adviser usage.

## 3. Documentation and validation

- [x] 3.1 Update setup docs, command help, and host instructions. Size: S. Depends on: 2.2. Check: documentation links, generated host files, and prompt budgets. Source: Discoverable adviser capabilities.
- [x] 3.2 Run repository checks and record observed outcomes. Size: M. Depends on: 3.1. Check: build, vet, race tests, lint, vulnerability, and change-record validation. Source: Built-in provider configuration; Typed single-question consultation; Bounded provider execution; Portable compatible history; Separate adviser usage; Discoverable adviser capabilities.
- [x] 3.3 Mark advisers optional and experimental, keep all disabled by default, and verify explicit opt-in. Size: S. Depends on: 3.1. Check: credential-only defaults, denied consultation without access, provider metadata, docs, and generated instructions. Source: Optional experimental advisers.
- [x] 3.4 Illustrate opt-in configuration, consultations, and optional checkpoints. Size: S. Depends on: 3.3. Check: Archify showcase and browser checks, light/dark inspection, and diagram export consistency. Source: Optional experimental advisers.
