---
type: regex
pattern: '^(?:[\x00-\x7F]{0,6144}|(?=[\s\S]{0,6016}$)(?:[\x00-\x7F]*[^\x00-\x7F]){1,64}[\x00-\x7F]*)$'
target: { source: file, path: artefacts/handoff.json }
---
