---
type: regex
pattern: '^## Purpose[ \t]*\n\s*(?=[^\s#])(?:(?!#)[\s\S]){49}(?:(?!#)[\s\S])*?[^\s#]'
flags: m
target: { source: file, path: testdata/profiles/openspec/changes/add-avatar-upload/specs/avatar-upload/spec.md }
---
