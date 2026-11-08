---
type: regex
pattern: '(?=[\s\S]*"changed_files"\s*:\s*\[\s*\])(?=[\s\S]*"findings"\s*:\s*\[\s*\{[\s\S]*"D1")(?=[\s\S]*"D2")'
target: { source: file, path: artefacts/handoff.json }
---
