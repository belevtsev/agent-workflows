---
type: regex
pattern: '(?=[\s\S]*\bA1\b)(?=[\s\S]*\b(?:blocker|major)\b)(?=[\s\S]*\b(?:R3|DR1)\b)(?=[\s\S]*\bgo\s+test\b[^\n]*-run\b)(?=[\s\S]*\bdiscard)(?=[\s\S]*\bnot[\s-]+probed\b)'
flags: i
target: { source: file, path: artefacts/adversarial-findings.md }
---
