---
type: regex
pattern: '(?=[\s\S]*"changed_files"\s*:\s*\[\s*\])(?=[\s\S]*"findings"\s*:\s*\[\s*\{[\s\S]*"T1")(?=[\s\S]*"T2")(?=[\s\S]*"T3")'
target: { source: file, path: artefacts/handoff.json }
---
