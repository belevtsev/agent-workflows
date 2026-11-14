---
type: regex
pattern: '(?=[\s\S]*^## 1\. \S)(?=[\s\S]*^- \[ \] 1\.1 \S)'
flags: m
target: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/tasks.md }
---
