---
type: regex
pattern: '(?=[\s\S]*\bS1\b)(?=[\s\S]*\bS2\b)(?=[\s\S]*\b(?:blocker|major)\b)(?=[\s\S]*\bscope creep\b)(?=[\s\S]*\bmissing failure mode\b)(?=[\s\S]*(?:^[ \t]*(?:#+[ \t]*|[-*][ \t]+)?(?:\*\*)?sources\b|\bsources\s+(?:read|reviewed|consulted|checked)\b))(?=[\s\S]*\b(?:not\s+reviewed|unreviewed)\b)'
flags: im
target: { source: file, path: artefacts/spec-review.md }
---
