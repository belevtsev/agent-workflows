---
type: regex
pattern: '^## Purpose[ \t]*\n\s*(?=[^\s#])(?:(?!#)[\s\S]){49}(?:(?!#)[\s\S])*?[^\s#]'
flags: m
target: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/specs/bookmark-export/spec.md }
---
