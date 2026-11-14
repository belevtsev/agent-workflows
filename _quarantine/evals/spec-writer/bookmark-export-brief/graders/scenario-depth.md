---
type: regex
pattern: '^(?:#{1,3}|#{5,}) *Scenario\b'
flags: m
match: not_contains
target: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/specs/bookmark-export/spec.md }
---
