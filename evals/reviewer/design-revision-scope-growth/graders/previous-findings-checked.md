---
type: regex
pattern: '(?=[\s\S]*\bD1\b)(?=[\s\S]*\bD2\b)(?=[\s\S]*\bD3\b)(?=[\s\S]*\bresolved\b)'
flags: i
target: { source: file, path: artefacts/design-review.md }
---
