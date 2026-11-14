---
type: regex
pattern: '(?=[\s\S]*\bF1\b)(?=[\s\S]*\bF2\b)(?=[\s\S]*\bF3\b)(?=[\s\S]*\bF4\b)(?=[\s\S]*\b(?:blocker|major)\b)(?=[\s\S]*\bR3\b)(?=[\s\S]*\bR4\b)(?=[\s\S]*\bgo\s+(?:-C(?:\s+|=)\S+\s+)?test\b)(?=[\s\S]*(?:^[ \t]*(?:#+[ \t]*|[-*][ \t]+)?(?:\*\*)?(?:sources|inputs)\b|\b(?:sources|inputs)\s+(?:read|reviewed|consulted|checked)\b))(?=[\s\S]*\b(?:not\s+reviewed|unreviewed)\b)'
flags: im
target: { source: file, path: artefacts/code-review.md }
---
