---
type: regex
pattern: '(?=[\s\S]*\bR1\b)(?=[\s\S]*\bR2\b)(?=[\s\S]*\bR3\b)(?=[\s\S]*\bR4\b)(?=[\s\S]*\bR5\b)(?=[\s\S]*\bR6\b)(?=[\s\S]*\bR7\b)(?=[\s\S]*\bR8\b)'
flags: i
target: { source: file, path: artefacts/requirement-map.md }
---
