---
type: regex
pattern: '\b(?:max(?:imum)?|limit(?:ed)?|at most|no (?:more|larger|bigger|greater) than|(?:larger|bigger|greater|more|smaller|less) than|exceed(?:s|ing)?|cap(?:ped)?)\b[^.\n]{0,40}?\d+(?:[.,]\d+)?\s*(?:[KMG]i?B|bytes?|kilobytes?|megabytes?|px|pixels)\b|\b(?:under|below|above|over|up to|within)\s+\d+(?:[.,]\d+)?\s*(?:[KMG]i?B|bytes?|kilobytes?|megabytes?|px|pixels)\b|\b\d+(?:[.,]\d+)?\s*(?:[KMG]i?B|bytes?|kilobytes?|megabytes?|px|pixels)\b[^.\n]{0,40}?\b(?:limit|maximum|max|cap|ceiling)\b'
flags: i
match: not_contains
target: { source: file, path: testdata/profiles/openspec/changes/add-avatar-upload/specs/avatar-upload/spec.md }
---
