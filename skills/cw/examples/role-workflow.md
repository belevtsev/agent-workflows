# A larger task: the role workflow

**Situation.** The user asks: "An order of ten items or more should cost a tenth less." This is
new behaviour that people will rely on. It is worth agreeing in writing what it must do before
any code exists.

## 1. Choose the way, and let the user decide

| The task | The way |
| --- | --- |
| A few lines with a plain cause, a rename, a change one test can pin down | The loop of this skill, with no roles |
| A defect or a refactoring that should get a second reader | The workflow, small path: tests, code, review, commit |
| New behaviour, or a change across several parts of the code | The workflow, full path: specification, design, tests, code, review, commit |

The workflow starts a separate agent for each role, so it uses more tokens and more time than
the loop. Say which way you propose and why, in one or two sentences, and let the user choose.
Most tasks are small. Do not start the workflow for a task the first row describes.

## 2. Start it

Load the skill `task` of this plugin with the user's request. In Claude Code that is
`/agent-workflows:task` followed by the request. The skill makes you the coordinator: you start
the roles, check what each one changed, keep the record, and ask the user for the approvals.
You write no specification, test or code yourself.

## 3. Where a task stands

The record of a task is a folder under `openspec/changes/`. Its files say how far the task is,
so a task can be continued in a new session. Here a proposal exists and nothing else.

```console
$ cw change status
add-discount: spec (3 missing; `cw change status add-discount` names them)
$ cw change status add-discount
add-discount: spec
  specs/<capability>/spec.md is missing
  reviews.md holds no review of the specification
  the approval of the specification is not recorded in reviews.md
content ids: spec <id>, design none, code <id>
$ cw change check add-discount
add-discount: the files obey their rules
```

When a folder like this already exists, do not start over. Load the skill `task`: it reads the
stage and continues there, and a finished stage is not repeated.

## 4. Specifications that already exist

A repository may already describe its behaviour, under `openspec/specs/` or in its own
documents. The role `lookup` finds them at the start, and the specification of the task is then
written against them: what is added, what changes, what goes away. Do not write a second
description of behaviour that is already specified.

## 5. What it cost

```console
$ cw tokens
no tokens are recorded in this clone; the plugin records them when a role or a session ends
```

Once the plugin has recorded the roles of a task, `cw tokens` prints one line for each task,
and `cw tokens --task add-discount` prints its roles, its stages and the share that went to the
workflow itself. A count that could not be read is printed as unknown, never as 0. Give the
user these numbers with the result, so that they can judge whether the workflow was worth it
for this kind of task.

## 6. The commit is gated like any other

The workflow ends with the same gate as the loop: the role `git` runs `cw run` and commits
through the hooks. `cw roles` prints the seven roles, the model of each and the paths each may
change.

<!-- A test replays this example from the fixture "task": internal/repocheck/skill_test.go -->
