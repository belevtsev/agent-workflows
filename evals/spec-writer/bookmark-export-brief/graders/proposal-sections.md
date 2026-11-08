---
type: regex
pattern: '(?=[\s\S]*^## +Why[ \t]*$)(?=[\s\S]*^## +What Changes[ \t]*$)(?=[\s\S]*^## +Capabilities[ \t]*$)(?=[\s\S]*^### +New Capabilities[ \t]*$)(?=[\s\S]*^### +Modified Capabilities[ \t]*$)(?=[\s\S]*^## +Impact[ \t]*$)(?=[\s\S]*^## +Non-goals[ \t]*$)(?=[\s\S]*^## +Sources[ \t]*$)(?=[\s\S]*^## +Open Questions[ \t]*$)'
flags: im
target: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/proposal.md }
---
