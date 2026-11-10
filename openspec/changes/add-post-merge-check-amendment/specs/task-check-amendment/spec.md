# task-check-amendment

## Purpose

Let a started task add `go test` checks, tied to a recorded merge from its base
branch, so that `cw task verify` can credit the merged packages and module files
while the recorded plan and its checks stay unchanged and closed.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- A Go module at the selected path (`go.mod` at its root) with packages
  `internal/a`, `internal/b`, `internal/c` and `internal/d`, in a Git repository
  whose branch `feature` was cut from `origin/main`.
- A substantial task started with `cw task start` under the default pack
  selection at baseline HEAD `B` (reachable from `origin/main`), so its recorded
  pack set applies pack `go` with evidence rule `go.package-test/v1`; with a
  baseline manifest, checkpoints required, `coordinator_id` `/root/coord`,
  and one requirement `r1` bound to check `unit` with argv
  `["go", "test", "./internal/a"]` and the default timeout.
- After start, `feature` committed `F` and then merge commit `M` with first parent
  `F` and second parent `O`, the tip of `origin/main`. Relative to `F`, `M`
  changed `go.mod` and `internal/b/b.go`, and both files are still changed since
  the baseline manifest.
- Check `widened` means argv `["go", "test", "./..."]`.
- "Changed" means changed since the task's baseline manifest (request.md:17-19).
  A "qualifying item" is a changed package, or a changed `go.mod`, `go.sum`,
  `go.work` or `go.work.sum`, that holds a file `M` changed relative to its first
  parent (coordinator answer OQ3).
- "Refused" means the command fails and no new record revision is written.

## ADDED Requirements

### Requirement: Amendment leaves the plan unchanged

An accepted amendment SHALL add checks to a started task without changing the recorded plan: the plan in every later revision MUST stay byte-identical to the plan in the first revision, and a record whose plan differs between revisions, including one that drops or narrows a plan check, MUST still fail to load.

Source: settled decision 3; request.md:9-11, 35-36, 41, 56-57; `internal/taskflow/storage.go:322-325`.

#### Scenario: Plan bytes are unchanged after an amendment

- **WHEN** an amendment adding `widened` for merge commit `M` and base ref `origin/main` is accepted
- **THEN** the deterministic JSON encoding of the plan in the new latest revision equals that of revision 1, and the plan still lists only check `unit`

#### Scenario: A check inserted into the plan in a later revision fails to load

- **WHEN** a later revision of an amended record stores a plan whose `checks` also lists a check `extra` with argv `["go", "test", "./internal/c"]`, an ID no plan check or amendment uses
- **THEN** loading the record fails with the existing "immutable task plan changed" refusal

#### Scenario: A plan check removed in a later revision fails to load

- **WHEN** a later revision stores a plan without check `unit` and without `unit` in `r1`'s check IDs
- **THEN** loading the record fails

#### Scenario: A plan check narrowed in a later revision fails to load

- **WHEN** a later revision stores a plan whose check `unit` has argv `["go", "test", "-run", "TestA", "./internal/a"]`
- **THEN** loading the record fails

### Requirement: Amendment applies only to an eligible task

An amendment SHALL be refused for a task whose plan kind is `small`, for a record without a baseline manifest, for an abandoned record, and for a record whose evidence rules include no Go package-test rule: a recorded pack set that names no `go.package-test/v1`, or, for a record without a pack set, a source to which the legacy Go rule does not apply.

Source: coordinator answer OQ8; coordinator decision on review finding F1(c); `internal/taskflow/abandon.go:9-18`; `internal/taskflow/executed.go:34-35, 56-69`; `internal/langpack/evaluate.go:30-46`; `internal/langpack/golang.go:24`.

#### Scenario: A small task is refused

- **WHEN** the task's plan kind is `small` and an otherwise qualifying amendment adding `widened` for `M` and `origin/main` is attempted
- **THEN** the amendment is refused

#### Scenario: A record without a baseline manifest is refused

- **WHEN** the record has no baseline manifest and an otherwise qualifying amendment is attempted
- **THEN** the amendment is refused

#### Scenario: A pack set without the Go rule is refused

- **WHEN** the task was started after `cw pack remove go`, so its recorded pack set names no `go.package-test/v1`, and an otherwise qualifying amendment adding `widened` for `M` and `origin/main` is attempted
- **THEN** the amendment is refused

#### Scenario: A record without a pack set where the Go rule does not apply is refused

- **WHEN** the record has no pack set, the selected path has no `go.mod` at its top, and an amendment adding `widened` for `M` and `origin/main` is attempted
- **THEN** the amendment is refused

#### Scenario: A record without a pack set where the Go rule applies is accepted

- **WHEN** the record has no pack set, `go.mod` is at the top of the selected path, and an amendment adding `widened` for `M` and `origin/main` is attempted
- **THEN** the amendment is accepted

#### Scenario: An abandoned record is refused

- **WHEN** the record was abandoned and an amendment adding `widened` for `M` and `origin/main` is attempted
- **THEN** the amendment is refused with the existing refusal containing "task was abandoned"

### Requirement: Amendment needs a merge from the base ref

An amendment SHALL be accepted only when Git resolves its merge commit and base ref, and the merge commit is committed, reachable from the current HEAD, not reachable from the baseline HEAD and not reachable from the base ref, and has a non-first parent reachable from the base ref; otherwise it MUST be refused. A dirty working tree MUST NOT by itself cause refusal.

Source: settled decision 2(a); review finding S3; coordinator answer OQ8; request.md:29-31, 38-40.

#### Scenario: A merge of the base branch is accepted

- **WHEN** the amendment names merge commit `M`, base ref `origin/main` and check `widened`
- **THEN** the amendment is accepted and one new record revision is written

#### Scenario: A merge from before start is refused

- **WHEN** the amendment names a merge commit `P` that is reachable from baseline HEAD `B`
- **THEN** the amendment is refused

#### Scenario: A commit that is not on the current branch is refused

- **WHEN** the amendment names a merge commit that is not reachable from the current HEAD
- **THEN** the amendment is refused

#### Scenario: A non-merge commit is refused

- **WHEN** the branch was rebased onto `origin/main` after start, so no merge commit exists after `B`, and the amendment names the rebased HEAD commit, which has one parent
- **THEN** the amendment is refused

#### Scenario: A merge of an unrelated branch is refused

- **WHEN** instead of `M`, `feature` committed merge commit `N` with first parent `B` (reachable from `origin/main`) and second parent the tip of branch `topic`, which is not reachable from `origin/main`, and the amendment names `N` and base ref `origin/main`
- **THEN** the amendment is refused

#### Scenario: HEAD as the base ref is refused

- **WHEN** the amendment names merge commit `M` and base ref `HEAD`
- **THEN** the amendment is refused, because `M` is reachable from `HEAD`

#### Scenario: The feature branch as the base ref is refused

- **WHEN** the amendment names merge commit `M` and base ref `feature`
- **THEN** the amendment is refused, because `M` is reachable from `feature`

#### Scenario: An unresolvable base ref is refused

- **WHEN** the amendment names merge commit `M` and base ref `origin/does-not-exist`
- **THEN** the amendment is refused

#### Scenario: An uncommitted merge is refused

- **WHEN** a merge of `origin/main` is in progress but not committed, and the amendment names the commit in `MERGE_HEAD` (the tip of `origin/main`) as the merge commit
- **THEN** the amendment is refused

#### Scenario: A project without Git is refused

- **WHEN** the selected path is not in a Git repository and an amendment naming a commit ID and base ref `origin/main` is attempted
- **THEN** the amendment is refused

#### Scenario: A dirty working tree is allowed

- **WHEN** `internal/a/a.go` has an uncommitted edit and the amendment names `M`, `origin/main` and `widened`
- **THEN** the amendment is accepted and its recorded source snapshot has `dirty` true

### Requirement: Added checks are validated like plan checks

An amendment SHALL add at least one check; each added check MUST pass the validation plan checks get, with a timeout of 1 to 3600 seconds that defaults to 300 when omitted, and the plan's checks plus all added checks MUST number at most 256; otherwise the amendment MUST be refused.

Source: review findings S7, S8; coordinator answer OQ5 (S8); `internal/taskflow/types.go:262-267, 302-317`.

#### Scenario: An amendment with no checks is refused

- **WHEN** an amendment names `M` and `origin/main` and adds no checks
- **THEN** the amendment is refused

#### Scenario: An omitted timeout defaults to 300 seconds

- **WHEN** the amendment adds `widened` without a timeout
- **THEN** the amendment is accepted and records `widened` with a timeout of 300 seconds

#### Scenario: An out-of-range timeout is refused

- **WHEN** the amendment adds `widened` with a timeout of 3601 seconds
- **THEN** the amendment is refused

#### Scenario: Exceeding the check cap is refused

- **WHEN** the plan holds 256 checks and the amendment adds `widened`
- **THEN** the amendment is refused

### Requirement: Added checks are credited direct go test runs

Every check an amendment adds SHALL be a direct `go test` argv that the check admission policy admits and that the existing Go evidence rule credits from argv alone (argv[0] go or go.exe, subcommand test, only allowlisted flags, no `-args`); an amendment with any other check MUST be refused. Admission does not consider GOFLAGS.

Source: settled decision 2(b); coordinator answer OQ4; request.md:15-18; `internal/langpack/golang.go:186-254`; `internal/taskflow/checks.go:53-87`.

#### Scenario: A wrapper command is refused

- **WHEN** the amendment adds check `widened` with argv `["make", "test"]`
- **THEN** the amendment is refused

#### Scenario: A filtering flag is refused

- **WHEN** the amendment adds check `widened` with argv `["go", "test", "-run", "TestX", "./..."]`
- **THEN** the amendment is refused

### Requirement: Each added check covers a new qualifying item

Each added check SHALL cover, by the patterns the Go rule credits from its argv, at least one qualifying item that no plan check, no check of an earlier amendment and no earlier check in the same amendment covers; a changed module file is covered only by `./...`. Otherwise the amendment MUST be refused.

Source: settled decision 2(b); review finding S6; coordinator answers OQ2 (S6), OQ3; request.md:15-18, 56-58; `internal/langpack/golang.go:26-104, 346-356`.

#### Scenario: A widened pattern covering go.mod and internal/b is accepted

- **WHEN** the amendment adds `widened`
- **THEN** the amendment is accepted, because `./...` covers qualifying items `go.mod` and `internal/b`, which `unit` does not cover

#### Scenario: A package-only check covering a merged package is accepted

- **WHEN** the amendment adds `b-tests` = `["go", "test", "./internal/b"]`
- **THEN** the amendment is accepted

#### Scenario: A check duplicating plan coverage is refused

- **WHEN** the amendment adds `again` = `["go", "test", "./internal/a"]`
- **THEN** the amendment is refused, because it covers nothing `unit` does not

#### Scenario: An import-path pattern covers nothing and is refused

- **WHEN** the amendment adds `widened` with argv `["go", "test", "example.com/m/..."]`, whose only pattern is neither `.` nor `./`-prefixed, so the Go rule credits no pattern from it
- **THEN** the amendment is refused

#### Scenario: A package changed only by a feature commit is refused

- **WHEN** `F` changed `internal/d/d.go`, `M` did not change `internal/d` relative to `F`, and the amendment adds `d-tests` = `["go", "test", "./internal/d"]`
- **THEN** the amendment is refused, because `internal/d` is not a qualifying item

#### Scenario: A package the merge changed but that is no longer changed is refused

- **WHEN** `M` also changed `internal/c/c.go` relative to `F`, a later commit restored `internal/c` to its baseline content, and the amendment adds `c-tests` = `["go", "test", "./internal/c"]`
- **THEN** the amendment is refused, because `internal/c` is not changed since the baseline

#### Scenario: A check already covered by an earlier amendment is refused

- **WHEN** an amendment adding `widened` was accepted, and a later amendment adds `b-tests` = `["go", "test", "./internal/b"]`
- **THEN** the later amendment is refused and the record keeps only the first amendment

#### Scenario: A later check in the same amendment that adds nothing is refused

- **WHEN** one amendment adds `widened` first and `b-tests` = `["go", "test", "./internal/b"]` second
- **THEN** the amendment is refused, because `b-tests` covers nothing `widened` does not

#### Scenario: A narrower check before a wider one in the same amendment is accepted

- **WHEN** one amendment adds `b-tests` = `["go", "test", "./internal/b"]` first and `widened` second
- **THEN** the amendment is accepted, because `widened` still covers qualifying item `go.mod`

### Requirement: Added check IDs are new

Every added check ID SHALL be an identifier accepted for plan check IDs and MUST differ from every plan check ID, from every check ID of an earlier amendment, and from the other IDs in the same amendment; an amendment that reuses an ID MUST be refused, so no existing check can be replaced, removed or narrowed.

Source: settled decision 3; request.md:35-36, 41, 56-57; `internal/taskflow/types.go:27, 305-309`.

#### Scenario: Narrowing a plan check by reusing its ID is refused

- **WHEN** the amendment adds check `unit` with argv `["go", "test", "-run", "TestA", "./internal/a"]`
- **THEN** the amendment is refused and plan check `unit` keeps argv `["go", "test", "./internal/a"]` and its binding to `r1`

#### Scenario: Replacing a plan check by reusing its ID is refused

- **WHEN** the amendment adds check `unit` with argv `["go", "test", "./..."]`
- **THEN** the amendment is refused and plan check `unit` keeps argv `["go", "test", "./internal/a"]`

#### Scenario: Narrowing an added check is refused

- **WHEN** an amendment adding `widened` was accepted, and a later amendment adds `widened` with argv `["go", "test", "./internal/b"]`
- **THEN** the later amendment is refused and `widened` keeps argv `["go", "test", "./..."]`

#### Scenario: Duplicate IDs within one amendment are refused

- **WHEN** one amendment adds `widened` with argv `["go", "test", "./..."]` and `widened` with argv `["go", "test", "./internal/b"]`
- **THEN** the amendment is refused

### Requirement: Amendment is recorded and visible

Each accepted amendment SHALL be stored in the task record, separate from the plan, with its added checks (ID, argv, timeout), the merge commit's full ID, the base ref name and the full commit ID it resolved to, a reason of 1 to 16384 bytes that is not blank, the plan's `coordinator_id`, the source snapshot and the time of acceptance; `cw task show` MUST display it.

Source: settled decision 3; review findings S4, S11; coordinator answer OQ5 (S8); request.md:38-40; `internal/taskflow/types.go:34`; `internal/taskflow/task.go:46-48`; `internal/taskflow/workers.go:152`.

#### Scenario: The recorded amendment carries its audit fields

- **WHEN** the amendment for `M`, `origin/main` (resolving to `O`), reason `merged origin/main; go.mod and internal/b changed` and check `widened` is accepted at current snapshot `S`
- **THEN** `cw task show` on the record shows one amendment with check `widened`, its argv and timeout 300, merge commit `M` and base commit `O` as full commit IDs, base ref `origin/main`, that reason, coordinator `/root/coord`, snapshot `S` and a non-zero UTC time

#### Scenario: The default coordinator is recorded

- **WHEN** the task was started from a plan without `coordinator_id` and a qualifying amendment is accepted
- **THEN** the amendment records coordinator `/root`

#### Scenario: A blank reason is refused

- **WHEN** the amendment for `M` and `origin/main` gives the reason `"   "`
- **THEN** the amendment is refused

#### Scenario: Reason length boundary

- **WHEN** one amendment gives a reason of 16384 bytes and another, otherwise identical attempt on a fresh copy of the record gives a reason of 16385 bytes
- **THEN** the first is accepted and the second is refused

### Requirement: Amendments are append-only

Amendments SHALL be append-only: a later revision MUST keep every earlier amendment unchanged and in order, and a record whose later revision removes, reorders or alters an earlier amendment, or stores an added check whose ID breaks the ID rule, MUST fail to load.

Source: settled decision 3; review finding S1; request.md:35-36, 41; `internal/taskflow/storage.go:256-335`.

#### Scenario: A revision that drops an amendment fails to load

- **WHEN** revision 5 holds the amendment adding `widened` and revision 6, otherwise valid, holds no amendment
- **THEN** loading the record fails

#### Scenario: A revision that changes an added check's argv fails to load

- **WHEN** revision 6 stores the amendment of revision 5 with `widened`'s argv changed from `["go", "test", "./..."]` to `["go", "test", "./internal/b"]`
- **THEN** loading the record fails

#### Scenario: A stored added check that collides with a plan check fails to load

- **WHEN** a stored revision holds an amendment whose added check has ID `unit`
- **THEN** loading the record fails

### Requirement: Amendment clears verification and joins the evidence binding

Recording an amendment SHALL clear any stored verification, and the evidence binding that a final checkpoint context hashes MUST include the amendments, so a final context prepared before an amendment is stale after it.

Source: coordinator answer OQ6; review finding S11; `internal/taskflow/task.go:378-393`; `internal/taskflow/consultation.go:201-206, 242-248`.

#### Scenario: A stored verification is cleared

- **WHEN** the record holds a complete verification and an amendment adding `widened` is accepted
- **THEN** `cw task show` on the record shows no stored verification

#### Scenario: A final context prepared before the amendment is stale

- **WHEN** the final context was prepared, then an amendment adding `widened` was accepted, and a final consultation for that earlier context is attached
- **THEN** the attachment is refused as stale against completed task evidence

### Requirement: Added checks run like plan checks

`cw task run` SHALL accept the ID of an added check and run its recorded argv exactly as it runs a plan check, recording the run under that ID; a stored run of an added check MUST carry that check's argv or the record fails to load, and an ID that is neither planned nor added MUST still be refused.

Source: settled decision 3; review finding S5; request.md:37-38; `internal/taskflow/checks.go:255-300`; `internal/taskflow/storage.go:368-382`.

#### Scenario: Running an added check records a run

- **WHEN** `cw task run` is given check `widened` after its amendment was accepted
- **THEN** a run with check ID `widened` and argv `["go", "test", "./..."]` is recorded and reaches a terminal status

#### Scenario: Running an unknown check is still refused

- **WHEN** `cw task run` is given check `nonexistent`
- **THEN** the run is refused and no run is recorded

#### Scenario: A stored run with the wrong argv fails to load

- **WHEN** a stored revision holds a run with check ID `widened` and argv `["go", "test", "./internal/b"]`
- **THEN** loading the record fails

### Requirement: Added check IDs are citable like plan check IDs

An added check ID SHALL be accepted wherever a plan check ID is: requirement assessments, the coordinator decision, worker briefs, worker results and worker acceptances; verify MUST validate a bound run of an added check against that check's argv; and the reviewer bundle MUST list each added check with its latest run as it lists plan checks.

Source: coordinator answer OQ7 (S5); review finding S5; `internal/taskflow/task.go:230-242, 341-366`; `internal/taskflow/workers.go:215-221, 763-769`; `internal/taskflow/bundle.go:360-374`.

#### Scenario: An assessment citing an added check completes

- **WHEN** `widened` and `unit` passed at the current source, requirement `r1` is assessed satisfied with check IDs `["unit", "widened"]`, and every other verify condition holds
- **THEN** the assessment is accepted and verify is complete, reporting no problem containing "check argv differs from the explicit plan"

#### Scenario: The coordinator decision citing an added check completes

- **WHEN** `widened` passed at the current source and the coordinator decision is resolved with check IDs `["widened"]`, and every other verify condition holds
- **THEN** the decision is accepted and verify reports no problem for it

#### Scenario: Worker records may cite an added check

- **WHEN** a worker brief has check IDs `["widened"]`, its result cites `["widened"]`, and the acceptance cites `["widened"]`
- **THEN** the assignment, the handoff and the acceptance are each accepted

#### Scenario: Worker records may not cite an unknown check

- **WHEN** a worker brief has check IDs `["nonexistent"]`
- **THEN** the assignment is refused

#### Scenario: The reviewer bundle lists an added check

- **WHEN** a reviewer bundle is written after `widened` passed at the current source
- **THEN** the bundle lists `widened` with its run ID, status `passed` and current true, and still lists `unit`

### Requirement: Verify credits an added check's run as a plan check's

For a changed substantial task, `cw task verify` SHALL add the latest run of each added check, when it passed at the current source snapshot, to the same passed-run list as plan checks' runs, and feed that list to the evidence rules the record applies (the pack set's rules with the fallback rule, or the legacy rules when the record has no pack set), crediting it exactly as a plan check's run, GOFLAGS check included.

Source: settled decision 3; review findings S10, F1; request.md:37-38, 53-55; `internal/taskflow/executed.go:50-69`; `internal/langpack/evaluate.go:26-46`; `internal/langpack/langpack.go:36-59`; `internal/langpack/golang.go:207-222`.

#### Scenario: Verify fails before the added check runs

- **WHEN** after the merge only `unit` has a passed run at the current source
- **THEN** verify is not complete and reports `changed go.mod has no passed go test ./... run at the current source` and `changed package internal/b has no passed go test run at the current source`

#### Scenario: Verify passes after the added check passes

- **WHEN** the amendment adding `widened` was accepted, the latest runs of `widened` and `unit` passed at the current source, and every other verify condition holds
- **THEN** verify is complete and reports neither of those two Go problems

#### Scenario: Verify fails before the added check runs on a record without a pack set

- **WHEN** the record has no pack set, the amendment adding `widened` was accepted, and only `unit` has a passed run at the current source
- **THEN** verify is not complete and reports `changed go.mod has no passed go test ./... run at the current source`

#### Scenario: Verify passes after the added check passes on a record without a pack set

- **WHEN** the record has no pack set, the amendment adding `widened` was accepted, the latest runs of `widened` and `unit` passed at the current source, and every other verify condition holds
- **THEN** verify is complete

#### Scenario: An added check's passed run satisfies the fallback rule

- **WHEN** the amendment adding `widened` was accepted, `widened`'s latest run passed at the current source and `unit` has no run
- **THEN** verify does not report `changed substantial task has no passed check run at the current source`

#### Scenario: A pass followed by a failure earns no credit

- **WHEN** `widened` passed and then failed at the same current source
- **THEN** verify reports `changed go.mod has no passed go test ./... run at the current source`

#### Scenario: GOFLAGS can void an added check's credit

- **WHEN** the latest run of `widened` passed at the current source with `GOFLAGS=-gcflags=all=-N` in its environment
- **THEN** verify reports `changed go.mod has no passed go test ./... run at the current source`

### Requirement: Admission is the only merge gate

After an amendment is admitted, `cw task run` and `cw task verify` SHALL NOT re-check its merge conditions: its added checks MUST stay runnable, and verify MUST credit their passed runs at the current source, when the merge commit is no longer reachable from HEAD or the base ref later contains it.

Source: coordinator answer to the spec-2 open question; review finding F2.

#### Scenario: Rewritten history does not void an added check

- **WHEN** after the amendment adding `widened` was accepted, `feature` is reset to `F` and `origin/main` is merged in again as a single-parent squash commit, so `M` is no longer reachable from HEAD while `go.mod` and `internal/b/b.go` are still changed
- **THEN** `cw task run widened` records a run, and once it and `unit` passed at the current source, verify reports neither `changed go.mod has no passed go test ./... run at the current source` nor `changed package internal/b has no passed go test run at the current source`

#### Scenario: A base ref that later contains the merge does not void an added check

- **WHEN** after the amendment adding `widened` was accepted, `origin/main` advances to a commit from which `M` is reachable
- **THEN** `cw task run widened` records a run, and once it and `unit` passed at the current source, verify reports no `changed go.mod has no passed go test ./... run at the current source` problem

### Requirement: An added check's latest run must pass

`cw task verify` SHALL report a problem when the latest run of an added check did not pass, exactly as it does for a plan check.

Source: settled decision 3; request.md:37-38; `internal/taskflow/task.go:438-442`.

#### Scenario: A failed added check blocks completion

- **WHEN** the latest run of `widened` has status `failed`
- **THEN** verify is not complete and reports `latest run of widened is failed`

### Requirement: Plan checks keep their obligations

An amendment SHALL NOT relax any plan check: a plan check whose latest run did not pass MUST still be reported by verify, and a satisfied requirement MUST still cite every plan check it names, whatever added checks passed.

Source: request.md:35-36; settled decision 3; `internal/taskflow/task.go:292-296, 405-442`.

#### Scenario: A failed plan check still blocks completion

- **WHEN** `widened` passed at the current source and the latest run of `unit` has status `failed`
- **THEN** verify is not complete and reports `latest run of unit is failed`

#### Scenario: An added check cannot stand in for a plan check

- **WHEN** requirement `r1` is assessed satisfied with check IDs `["widened"]` only
- **THEN** the assessment is refused with the existing refusal containing "omits planned check unit"

### Requirement: Verify credits only local runs at the current source

`cw task verify` SHALL credit the Go evidence rule only with check runs stored in the task record that passed at the current source snapshot, and MUST keep in the changed set every file changed since the baseline manifest, including a file whose content equals the base side of a merge.

Source: settled decision 1; review finding S9; request.md:42-48.

#### Scenario: A file brought in unchanged from the base branch still needs a local run

- **WHEN** `M` also brought `internal/c/c.go` with content identical to `O`'s, and no plan or added check covers `internal/c`
- **THEN** verify reports `changed package internal/c has no passed go test run at the current source`
