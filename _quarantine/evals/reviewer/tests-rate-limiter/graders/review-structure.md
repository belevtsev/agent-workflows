---
type: regex
pattern: '(?=[\s\S]*\bT1\b)(?=[\s\S]*\bT2\b)(?=[\s\S]*\bT3\b)(?=[\s\S]*\b(?:blocker|major)\b)(?=[\s\S]*\bR4\b)(?=[\s\S]*\bTestCountMatchesAllowed\b)(?=[\s\S]*\bTestConcurrentAllow\b)(?=[\s\S]*(?:^[ \t]*(?:#+[ \t]*|[-*][ \t]+)?(?:\*\*)?(?:sources|inputs)\b|\b(?:sources|inputs)\s+(?:read|reviewed|consulted|checked)\b))(?=[\s\S]*\b(?:not\s+reviewed|unreviewed)\b)'
flags: im
target: { source: file, path: artefacts/test-review.md }
---
