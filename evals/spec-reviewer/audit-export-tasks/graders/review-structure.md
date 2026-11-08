---
type: regex
pattern: '(?=[\s\S]*\bS1\b)(?=[\s\S]*\bS2\b)(?=[\s\S]*\bS3\b)(?=[\s\S]*\b(?:blocker|major)\b)(?=[\s\S]*\bunsizeable or unverifiable task\b)(?=[\s\S]*\bformat\b)(?=[\s\S]*(?:^[ \t]*(?:#+[ \t]*|[-*][ \t]+)?(?:\*\*)?sources\b|\bsources\s+(?:read|reviewed|consulted|checked)\b))(?=[\s\S]*\b(?:not\s+reviewed|unreviewed)\b)'
flags: im
target: { source: file, path: artefacts/spec-review.md }
---
