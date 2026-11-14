---
type: llm
focus: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/tasks.md }
---

PASS if the tasks are grouped under numbered headings (`## 1. <name>`, ...)
of checkbox items (`- [ ] 1.1 <task>`, ...), and every task names its size
as S, M or L, names the tasks it depends on (or says it has none), and names
its check as either a command or the spec scenarios it must pass (by
scenario name), such that the check would fail if the task were not done;
every dependency points to an earlier task; and no task covers something
outside the brief (JSON or HTML export, import, team export, emailing) or
writes a `design.md`.
FAIL if any task lacks a size, dependencies or a check, a check is vague
("works", "verify manually", "tests pass" with no command or scenario
named), a task depends on a later task, a task is larger than one session
(L is the largest allowed size), or a task covers out-of-scope work. Whether
a task also mentions tests or documentation does not affect the verdict.
