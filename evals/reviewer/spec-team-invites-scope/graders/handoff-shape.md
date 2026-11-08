---
type: regex
pattern: '(?=[\s\S]*"changed_files"\s*:\s*\[\s*\])(?=[\s\S]*"findings"\s*:\s*\[\s*\{[\s\S]*"S1")(?=[\s\S]*"S2")'
target: { source: file, path: artefacts/handoff.json }
---
