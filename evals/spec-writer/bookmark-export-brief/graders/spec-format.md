---
type: regex
pattern: '(?=[\s\S]*^## Purpose[ \t]*$)(?=[\s\S]*^## ADDED Requirements[ \t]*$)(?=[\s\S]*^### Requirement: \S)(?=[\s\S]*^#### Scenario: \S)(?=[\s\S]*^[ \t]*- \*\*WHEN\*\* )(?=[\s\S]*^[ \t]*- \*\*THEN\*\* )'
flags: m
target: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/specs/bookmark-export/spec.md }
---
