---
type: regex
pattern: '(?=[\s\S]*\bD1\b)(?=[\s\S]*\bD2\b)(?=[\s\S]*\b(?:blocker|major)\b)(?=[\s\S]*\bconflicting_timeout\b)(?=[\s\S]*(?:^[ \t]*(?:#+[ \t]*|[-*][ \t]+)?(?:\*\*)?(?:sources|inputs)\b|\b(?:sources|inputs)\s+(?:read|reviewed|consulted|checked)\b))(?=[\s\S]*\b(?:not\s+reviewed|unreviewed)\b)'
flags: im
target: { source: file, path: artefacts/design-review.md }
---
