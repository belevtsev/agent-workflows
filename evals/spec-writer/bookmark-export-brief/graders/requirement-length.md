---
type: regex
pattern: '^### Requirement:[^\n]*\n\s*(?=\S)(?:(?!#{2,4} )[\s\S]){500}(?:(?!#{2,4} )[\s\S])*?(?!#{2,4} )\S'
flags: m
match: not_contains
target: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/specs/bookmark-export/spec.md }
---
