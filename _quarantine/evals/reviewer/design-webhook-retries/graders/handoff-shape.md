---
type: regex
pattern: '(?=[\s\S]*"changed_files"\s*:\s*\[\s*\])(?=[\s\S]*"findings"\s*:\s*\[\s*\{[\s\S]*"D1")(?=[\s\S]*"D2")(?=[\s\S]*"D3")'
target: { source: file, path: artefacts/handoff.json }
---
