---
type: regex
pattern: '(?=[\s\S]*^## MODIFIED Requirements[ \t]*$)(?=[\s\S]*^## ADDED Requirements[ \t]*$)(?=[\s\S]*^### Requirement: Default link lifetime[ \t]*$)(?=[\s\S]*^#### Scenario: \S)'
flags: m
target: { source: file, path: testdata/shortlinks/openspec/changes/shorten-default-link-lifetime/specs/link-expiry/spec.md }
---
