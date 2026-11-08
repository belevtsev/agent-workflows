---
type: regex
pattern: '^### +Modified Capabilities[ \t]*\n(?:(?!##)[^\n]*\n)*?[^\n]*\blink-expiry\b'
flags: m
target: { source: file, path: testdata/shortlinks/openspec/changes/shorten-default-link-lifetime/proposal.md }
---
