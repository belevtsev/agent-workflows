# Proposal: add-post-merge-check-amendment

## Why

A task plan's checks are fixed at `cw task start`: the skill and the global
workflow say the plan cannot change after start
(`skills/task-orchestration/SKILL.md:39`,
`skills/task-orchestration/references/global-workflow.md:142`), and loading a
record whose plan differs between revisions fails with "immutable task plan
changed" (`internal/taskflow/storage.go:322-325`). `cw task run` executes only
checks in the plan (`internal/taskflow/checks.go:266-275`).

For a substantial task whose source changed since its baseline, `cw task verify`
applies evidence rules (`internal/taskflow/task.go:494-499`,
`internal/taskflow/executed.go:23-70`). It builds one list of passed runs, the
latest run of each plan check when it passed at the current source
(`internal/taskflow/executed.go:50-55`), and feeds it to the record's rules: the
legacy rules when the record has no pack set (`executed.go:56-61`), otherwise
the fallback any-passed-run rule plus each rule the recorded pack set names
(`executed.go:62-69`, `internal/langpack/evaluate.go:26-46`). `cw task start`
always records a pack set (`internal/cli/task_commands.go:115`), and the default
selection applies the `go` pack's rule `go.package-test/v1`
(`skills/task-orchestration/references/global-workflow.md:110-121`). Under the
Go rule every changed Go package needs a passed, credited `go test` run at the
current source that covers it, and a changed `go.mod`, `go.sum`, `go.work` or
`go.work.sum` needs a passed `go test ./...` run
(`internal/langpack/golang.go:13-51`); only allowlisted direct `go test` argv
earns credit (`internal/langpack/golang.go:186-254`).

Merging the base branch (for example `origin/main`) into a feature branch
mid-task brings in changed packages, and often module files, that no fixed plan
check covers. Verify then cannot pass and nothing in the record can be changed
to fix it. Two real tasks had to abandon their records to publish, losing the
gate (request.md:21-25).

## What Changes

- A substantial task with a baseline manifest, whose evidence rules include the
  Go package-test rule, can append, after start, an
  amendment that adds `go test` checks, tied to a recorded merge commit from a
  named base ref (request.md:29-31, 38-40; settled decision 2; coordinator
  answer OQ8; coordinator decision on review finding F1(c)).
- The merge commit must be committed, reachable from the current HEAD, not
  reachable from the baseline HEAD or from the base ref, with a non-first parent
  reachable from the base ref (settled decision 2(a); review finding S3).
- Each added check must be an allowlisted direct `go test`, validated like a
  plan check (timeout 1..3600 s, default 300; at most 256 checks in plan and
  amendments together), that covers a changed package or module file the merge
  changed relative to its first parent and that no earlier check covers
  (settled decision 2(b); coordinator answers OQ2, OQ3, OQ5).
- Amendments live in a separate append-only record member recording the added
  checks, merge commit, base ref and the commit it resolved to, reason, the
  plan's `coordinator_id`, source snapshot and time; the recorded plan stays
  byte-identical (settled decision 3; review findings S4, S11).
- Recording an amendment clears a stored verification, and amendments join the
  evidence binding the final checkpoint context hashes (coordinator answer OQ6).
- Added check IDs work wherever plan check IDs do: `cw task run`, assessments,
  the coordinator decision, worker briefs, results and acceptances, and the
  reviewer bundle (coordinator answer OQ7).
- `cw task verify` requires an added check's latest run to pass like a plan
  check's, and puts its latest passed run in the same passed-run list as plan
  checks' runs, so it feeds whichever evidence rules the record applies (the
  pack set's rules with the fallback rule, or the legacy rules for a record
  without a pack set) exactly as a plan check's run does (request.md:37-38;
  settled decision 3; review findings S10, F1).
- Admission is the only gate on the merge: after it, `cw task run` and
  `cw task verify` do not re-check the merge conditions, so a history rewrite or
  a moved base ref does not void an added check (coordinator answer to the
  spec-2 open question; review finding F2).
- Verify still credits only local passed runs at the current source; files
  identical to the base side stay in the changed set (request.md:42-48; settled
  decision 1).
- Removing, replacing or narrowing a check stays refused (request.md:35-36, 41).
- The plan-is-fixed rule in `skills/task-orchestration/SKILL.md`,
  `skills/task-orchestration/references/global-workflow.md` and
  `docs/operations.md`, and the `check_ids` rule in
  `skills/task-orchestration/references/worker-handoffs.md:66`, are updated,
  with a `CHANGELOG.md` entry under `## [Unreleased]` (request.md:59-62; review
  finding S5).
- **BREAKING** (record compatibility): a `cw` release older than this change
  rejects unknown record members (`internal/taskflow/types.go:222-233`), so it
  cannot read a record carrying amendments; `docs/hooks.md` and `CHANGELOG.md`
  say so, as `docs/hooks.md:116-118` already does for `source_sha256` and
  `abandonment` (coordinator answer OQ10).

## Capabilities

### New Capabilities

- `task-check-amendment`: recorded, merge-bound addition of `go test` checks to
  a started task, and how task commands and `cw task verify` treat them.

### Modified Capabilities

None. The repository has no `openspec/specs/`; this change folder is the only
OpenSpec content and was created without `openspec init`.

## Impact

- `internal/taskflow`: record types and load-time validation
  (`types.go:189-220`, `storage.go:256-404`); check execution
  (`checks.go:258-300`); completion evidence and run binding
  (`task.go:230-242, 341-467`, `executed.go:50-69`); evidence binding and the
  final consultation (`task.go:378-393`, `consultation.go:201-206, 242-248`);
  worker briefs and acceptances (`workers.go:215-221, 763-769`); the reviewer
  bundle (`bundle.go:360-374`).
- `internal/langpack/golang.go`: the coverage and crediting rule the admission
  test reuses (`credited`, `covers`, `changedWork`, `packageOf`);
  `internal/langpack/evaluate.go` and `internal/taskflow/packs.go`: which
  evidence rules a record applies, for the eligibility test.
- `internal/projectctx`: Git probes for the merge conditions (the snapshot
  already carries `Head` and `Branch`, `internal/projectctx/types.go:12-22`).
- `internal/cli`: a new task subcommand, following `cw task abandon`
  (`internal/cli/task_abandon.go`, `internal/taskflow/abandon.go`).
- Docs: `skills/task-orchestration/SKILL.md:39`,
  `skills/task-orchestration/references/global-workflow.md:142`,
  `skills/task-orchestration/references/worker-handoffs.md:66`,
  `docs/operations.md:216-231`, `docs/hooks.md:116-118`, `CHANGELOG.md`
  `## [Unreleased]`.
- The publication hook (`internal/cli/hook.go`) changes only through verify's
  result and the cleared verification.

## Non-goals

- Crediting the base branch's CI. `cw task verify` does not treat a merge's
  base-side changes as tested because the base branch's CI passed them, does not
  drop files identical to the base side from the changed set, and does not query
  GitHub or any network service. Reason: the merged tree is a combination the
  base branch's CI never ran, and verify is an offline local gate (settled
  decision 1; request.md:42-48).
- Removing, replacing, narrowing or altering any plan check or previously added
  check, or changing which checks a plan requirement names (request.md:35-36, 41;
  settled decision 3).
- Adding a check other than an allowlisted direct `go test` (for example a
  linter or `make test`) through an amendment (settled decision 2(b)).
- Adding a check without a qualifying merge commit, for example after a rebase
  onto the base branch, which leaves no merge commit (settled decision 2(a)).
- Judging GOFLAGS at admission; GOFLAGS stays verify's concern, as for plan
  checks (coordinator answer OQ4).
- Amending a `small` task (coordinator answer OQ8).
- CLI command naming, file layout, record field names and exact refusal
  wording; the architect chooses them (coordinator brief; coordinator answer
  OQ9).

## Sources

- Request: `/private/tmp/claude-501/-Users-pbelevtsev-Documents-agent-workflows--claude-worktrees-goofy-jepsen-f167f1/75ac9512-7d89-4d3c-864c-63d36509521f/scratchpad/request.md`,
  sha256 `81b9aff19aa32c5956d224033b43a04e6261f5f6d2e52e04bef8203dc61265dd`
  (cited as request.md:LINE).
- Coordinator brief for assignment `spec-1`, settled decisions 1-3 (cited as
  "settled decision N").
- Spec review `spec-review-1`
  (`/private/tmp/claude-501/-Users-pbelevtsev-Documents-agent-workflows--claude-worktrees-goofy-jepsen-f167f1/75ac9512-7d89-4d3c-864c-63d36509521f/scratchpad/art/result-spec-review-1.json`),
  findings S1-S11, all accepted (cited as "review finding SN").
- Coordinator brief for assignment `spec-2`, answers to the spec-1 open
  questions (cited as "coordinator answer OQN").
- Follow-up spec review `spec-review-2`
  (`/private/tmp/claude-501/-Users-pbelevtsev-Documents-agent-workflows--claude-worktrees-goofy-jepsen-f167f1/75ac9512-7d89-4d3c-864c-63d36509521f/scratchpad/art/result-spec-review-2.json`),
  findings F1-F4, and the coordinator brief for assignment `spec-3` (decision on
  F1(c) and the answer to the spec-2 open question).
- Repository `belevtsev/agent-workflows` worktree at HEAD `5349f76` (request base
  `ca478c9`); every `path:line` above is at `5349f76`.

## Open Questions

None. Every earlier question is answered by the coordinator and applied in the
spec.
