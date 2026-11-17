# Tasks

## 1. Optional diagnostics

- [x] 1.1 Write local regression tests for exact captured totals, missing attribution and interval overlap. Size: M. Depends on: none. Check: test. Source: Inspect captured workflow evidence; Report managed interval coverage.
- [x] 1.2 Implement explicit read-only diagnostic projection from existing token, check and managed interval records. Size: M. Depends on: 1.1. Check: test, review. Source: Inspect captured workflow evidence; Report managed interval coverage.

## 2. Source-linked optional notes

- [x] 2.1 Test bounded recognized optional note projection and repeated-failure grouping across content and policy changes. Size: M. Depends on: 1.2. Check: test. Source: Project optional consequential notes; Investigate repeated check failures.
- [x] 2.2 Implement optional original-referenced note projection and same-check/content/policy failed-attempt groups. Size: M. Depends on: 2.1. Check: test, review. Source: Project optional consequential notes; Investigate repeated check failures.

## 3. Optional budget metadata

- [x] 3.1 Write tests for unit-specific full/small budgets, invalid inputs, completeness and known captured exceedance. Size: M. Depends on: 2.2. Check: test. Source: Record estimates and sourced declared limits; Compare budgets with incomplete actuals.
- [x] 3.2 Implement explicit budget set/show/reset with source-preserving estimate and limit display. Size: M. Depends on: 3.1. Check: test, review. Source: Record estimates and sourced declared limits; Compare budgets with incomplete actuals.

## 4. Guidance and validation

- [x] 4.1 Document concrete CLI and optional note examples; regenerate role host files and command reference. Size: S. Depends on: 3.2. Check: test, review. Source: Preserve existing workflow authority.
- [x] 4.2 Run local required checks, map passing tests and record remaining limitations without paid execution. Size: M. Depends on: 4.1. Check: build, vet, test, lint, record, review. Source: Preserve existing workflow authority.
