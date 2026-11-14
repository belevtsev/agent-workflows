# Design: add-role-workflow

Status: written on 14 November 2026 for an implementation that runs without
the owner. The twelve Open Questions of the proposal take their recommended
answers; the owner can change any of them afterwards.

## Context

- **Problem.** The owner wants named roles with a model each, a task that goes
  from a specification to tested code in any language, and a count of the
  tokens the workflow itself costs. Today's roles assume Go, check nothing a
  role changed, keep a task as a lock, and are 150,080 characters of text.
- **Outcome.** Three small commands and eight texts on top of the gate.
- **Base.** The branch and the commit of the first change's design. This
  change starts when the gate's commands exist, because it uses the snapshot,
  the differing paths, `cw.toml` and the record store.
- **What cannot be done alone.** Nothing here was run with a model, and
  nothing was checked on Codex. The evaluation (Success measures 1 and 7)
  needs model runs the owner pays for.

## Decision

The workflow is text for the developer's coding agent, and `cw` checks
afterwards what a program can check. Three alternatives were rejected:

- **A `cw` that calls models itself.** It would need keys, a network
  connection and an agent loop, and the gate promises to use no network.
- **Today's task store.** State outside the folder is what blocked finished
  work (H2, H9).
- **Limits enforced before a role writes.** A host can remove a tool from a
  role but cannot limit a shell to some paths, so the check runs after the
  role ends, on the paths that differ between two snapshots.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `internal/roles` | The seven roles, their models, their write limits, `cw-roles.toml`, and the texts | `internal/decl`, `internal/gitx` | yes | `internal/modelpolicy` on `workflow-v3`: it validates profiles and scopes this design does not have; the path patterns of `internal/decl` are reused |
| `internal/change` | Reads a change folder; decides its stage; checks its rules | `internal/decl` | yes | `internal/skillcheck` on `workflow-v3`: it checks skill files, not change folders |
| `internal/tokens` | Reads the host's record of a session; stores counts; reports them | `internal/store` | yes | `internal/usagelog` on `workflow-v3`: its two readers are the starting point; prices, budgets and history are not carried |
| `internal/cli` | The commands `cw roles`, `cw change` and `cw tokens` | `internal/roles`, `internal/change`, `internal/tokens` | no | |
| `internal/decl` | Path patterns and strict TOML decoding | none | no | |
| `internal/gitx` | Snapshot and differing paths | none | no | |
| `internal/store` | The directory `cw/` in the common git directory | none | no | |

## Interfaces

### `cw roles [--json]`

Prints the seven roles: job, what each may write, model and effort on each
host, and the size of its text. JSON: `{"roles": [{"id", "job", "writes",
"text_chars", "models": {"claude": {"model", "effort"}, "codex": {...}}}]}`.

### `cw-roles.toml`

```toml
version = 1

[paths]
tests = ["tests/**", "**/*_test.go"]

[claude.developer]
model = "sonnet"
effort = "high"
```

Hosts are `claude` and `codex`; roles are the seven ids; keys are `model` and
`effort`. Anything else is refused with the file, the line and the rule.

### `cw roles check <role> --from <tree> [--task <task>]`

Takes the snapshot, lists the paths that differ from `<tree>` with their kind
(added, changed, removed), and compares each with the role's limit. Exit 0, or
5 with the paths outside the limit, or 1 when the role needs the test paths
and `cw-roles.toml` does not name them.

### The change folder

| File | Written by | Format |
| --- | --- | --- |
| `proposal.md` | `spec-writer` | Free text. |
| `specs/<capability>/spec.md` | `spec-writer` | `## ADDED`, `## MODIFIED` or `## REMOVED Requirements`; `### Requirement: <name>`; a `Source:` line; `#### Scenario: <name>` with one `- **WHEN**` and one `- **THEN**` line. |
| `design.md` | `architect` | Free text, plus a table under `## Building blocks` with the columns Block, Responsibility, Depends on, New, Reuse considered, and a table under `## Rules` with the columns Rule and Checked by. |
| `tasks.md` | `architect`; the coordinator ticks the boxes | `- [ ] N.M <text>. Size: S\|M\|L. Depends on: <ids or none>. Check: <...>. Source: <...>.` under `## N. <group>`. |
| `tests.md` | `quality-engineer` | A table with the columns Scenario, Test file, Text. A row that is not automated holds `not automated` as its file and a reason as its text. |
| `reviews.md` | the coordinator | Three tables: under `## Reviews` the columns Stage, Round, Date; under `## Findings` the columns Id, Stage, Round, Severity, Where, Finding, Disposition, Reason; under `## Approvals` the columns What, Date. |

`cw change status [<task>] [--json]` prints the stage and what is missing.
`cw change check [<task>]` exits 0 or 5 and names the file, the line and the
rule.

### Tokens

- The plugin's hooks for the end of a role and the end of a session run
  `cw tokens record`, which reads the host's notice from standard input.
- For Claude Code the notice names the transcript of the role or of the
  session. `cw` sums the `usage` of each answer once, by its message id, reads
  the model from the answer and the label from the first line of the first
  message, and writes `cw/tokens/<session>/<agent>.json`. Writing the same
  notice twice gives the same file.
- `cw tokens [--task <task>] [--json]` reads those files. JSON: `task`,
  `total`, `complete`, `writing`, `flow` (`coordination`, `review`, `lookup`,
  `git`, `rework`), `flow_share`, `roles`, `stages`, `unlabelled`, `sessions`.

## Rules

| Rule | Checked by |
| --- | --- |
| No package outside `internal/cli` imports `internal/cli` | review |
| `cw` links no network package | review |
| A role text names no language, build tool, test framework or package manager | review |
| A host's role file is generated from the role text and never edited by hand | review |

"review" stands for a Go test in each case; this repository's `cw.toml` does
not exist yet, so no check id can be named. Task 6.3 replaces the word when it
does.

## Validation

| What | How | Where it runs | What it shows, and what it does not |
| --- | --- | --- | --- |
| The scenarios without "(evaluation)" | Tests that drive `cw` in temporary repositories, and tests of the plugin's files | Linux, locally | The commands and the files. |
| The token report | Recorded sessions kept as test data, built to the shape today's reader expects | Linux | That the sums are right for that shape. Not that a current host still writes it. |
| The scenarios with "(evaluation)" | Model runs of the two paths in three repositories | Not run | Nothing yet. This is the owner's step. |
| Codex | Nothing | Not run | Nothing. |

## Failure behaviour

| Condition | What `cw` does |
| --- | --- |
| `cw-roles.toml` is invalid | Every `cw roles` command exits 1 with the file, the line and the rule. |
| A role's transcript cannot be read | Its tokens are `unknown`, and every sum says that it is incomplete. |
| A brief has no label | Its tokens are listed as `unlabelled`. |
| A change folder is half written | `cw change check` passes when the files it has obey their rules; `cw change status` names the stage. |
| The repository has no `cw.toml` | `cw tokens record` stores nothing; the other commands work. |
