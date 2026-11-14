---
type: regex
pattern: '\b(?:defaultTTL|NewLink|ErrExpired|ExpiresAt)\b'
match: not_contains
target: { source: file, path: testdata/shortlinks/openspec/changes/shorten-default-link-lifetime/specs/link-expiry/spec.md }
---
