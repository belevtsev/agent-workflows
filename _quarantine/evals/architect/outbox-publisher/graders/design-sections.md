---
type: regex
pattern: '(?=[\s\S]*^#+ *Options considered)(?=[\s\S]*^#+ *Decision)(?=[\s\S]*^#+ *Component boundaries)(?=[\s\S]*^#+ *Failure behaviour)(?=[\s\S]*^#+ *Rollout and rollback)(?=[\s\S]*^#+ *Verification plan)'
flags: im
target: { source: file, path: artefacts/design.md }
---
