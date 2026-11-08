---
type: regex
pattern: '(?=[\s\S]*"changed_files"\s*:\s*\[\s*\])(?=[\s\S]*"findings"\s*:\s*\[\s*\{[\s\S]*"F1")(?=[\s\S]*"F2")(?=[\s\S]*"F3")(?=[\s\S]*"F4")'
target: { source: file, path: artefacts/handoff.json }
---
