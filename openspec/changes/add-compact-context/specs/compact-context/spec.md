# compact-context

## ADDED Requirements

### Requirement: Bounded Go collection

Kitsu SHALL collect named file reads, literal or explicit Go-regexp searches and Git state
through a versioned non-executable JSON interface, with at most 32 operations and four read
workers. It MUST preserve deterministic ordering, line references and explicit partial errors.

Source: approved built-in Go engine and repository-exploration scope.

#### Scenario: Collection retains ranges and deterministic search facts

- **WHEN** a request combines reads, searches, Git queries and an absent path
- **THEN** ordered results preserve source references and report individual errors without hiding valid evidence.

#### Scenario: Executable or malformed requests are refused

- **WHEN** a request contains unknown operations, executable fields, invalid ranges or excessive operations
- **THEN** collection refuses it before execution.

### Requirement: Contained text sources

Collection MUST read only repository-scoped nonignored regular UTF-8 sources without NULs,
bounded by file and retained-result limits. Paths and symlinks MUST NOT escape containment.

Source: accepted scope and host/role authority boundaries.

#### Scenario: Ignored or escaping sources expose no content

- **WHEN** ignored files or escaping symlinks are requested
- **THEN** no content is returned and the outcome identifies unavailable sources.

#### Scenario: Unsupported text and excessive results stay explicit

- **WHEN** a source is binary, invalid UTF-8, oversized or generates excessive results
- **THEN** errors or partial skip counts are returned without unbounded result retention.

### Requirement: Exact budgets and expansion

Responses SHALL default to 16 KiB including metadata and JSON framing. Required outcomes,
constraints and uncertainties MUST NOT be silently discarded. Retained omissions SHALL have
artifact/item-bound cursors and expansion MUST NOT rerun operations or checks.

Source: approved bounded observations with expandable evidence.

#### Scenario: Pages recover every retained UTF-8 fragment

- **WHEN** evidence needs multiple pages or long-line fragments
- **THEN** cursors recover the original retained evidence with valid text and exact output bounds.

#### Scenario: Failed checks retain tail diagnostics and full expansion

- **WHEN** a failed check prints noisy output
- **THEN** its compact preview retains tail diagnostics and expansion recovers its normal retained log from the beginning.

### Requirement: Private fresh artifacts

Artifacts SHALL be isolated per worktree, private where platform modes apply, bounded to
32 MiB each and 64 MiB total, expire after 24 hours and evict oldest entries first. They MUST
reject stale source, HEAD, ignore-policy, external skill-policy and check-record observations.

Source: accepted private cache and actual dependency invalidation.

#### Scenario: Content HEAD and ignore changes invalidate artifacts

- **WHEN** source content, HEAD or effective ignore exclusions change
- **THEN** stale artifacts are refused and fresh collection receives a different identity.

#### Scenario: Git normalization cannot hide a raw-source change

- **WHEN** CRLF or clean-filter normalization preserves a tree ID while raw text changes
- **THEN** old source observations are refused.

#### Scenario: Cache eviction expiry and worktree isolation are observable

- **WHEN** cache entries expire, exceed the aggregate limit or are requested from another worktree
- **THEN** unavailable entries fail explicitly and caches remain bounded and isolated.

#### Scenario: Source changes during preparation prevent publication

- **WHEN** repository content changes after the artifact baseline was captured
- **THEN** the artifact is not published against that old baseline.

### Requirement: Complete fresh-worker briefs

Only the coordinator SHALL prepare worker briefs with task/stage/round labels, effective
model/reasoning, write limits, original selected skills and hashes, allowed advisers and factual
task state. Selection receipts MUST precede the returned baseline. Native role instructions
MUST NOT be duplicated, and small history MUST NOT imply a resumable workflow stage.

Source: approved role handoff scope and existing role/skill workflow rules.

#### Scenario: Brief preserves constraints and receipt ordering

- **WHEN** the coordinator prepares a fresh role
- **THEN** its brief retains current settings and authority boundaries and its baseline includes the selection receipt.

#### Scenario: Invalid brief requests write no receipt

- **WHEN** role/stage/round, workflow, skills or the mandatory-output budget are invalid
- **THEN** preparation fails before writing a selection receipt.

#### Scenario: External selected skills invalidate old briefs

- **WHEN** selected skill bytes change outside the repository
- **THEN** the old brief is refused despite an unchanged Git working tree.

### Requirement: Equivalent declared checks

Compact checks SHALL use the existing sequential runner, declaration order, content evidence,
retained logs, cancellation and exit behavior. Successful previews MAY omit logs; failures,
timeouts, source changes and skipped/implied outcomes MUST remain explicit.

Source: approved inclusion of declared checks without altered execution semantics.

#### Scenario: Compact checks preserve ordinary results

- **WHEN** the same declared check passes, fails, times out or changes source
- **THEN** normal and compact execution retain equivalent outcome and evidence semantics.

#### Scenario: Forced base-implied runs keep executed logs

- **WHEN** a check implied by a base revision is explicitly forced and fails
- **THEN** it is reported as executed and its diagnostics remain expandable.

#### Scenario: Large retained logs preserve check exits

- **WHEN** several checks print logs at the runner's retention limit
- **THEN** compact reporting retains normal check outcomes without copying all full logs into its artifact.

#### Scenario: New evidence or expiry invalidates a verdict

- **WHEN** a run finishes after the captured verdict or deciding evidence expires
- **THEN** that report cannot retain stale verified authority under the newer observation.

### Requirement: Explicit rollout and compatibility

Compact mode SHALL require an explicit task-start flag or request. Help/completion and docs
MUST describe it without runtime setup; bare context hook ingress MUST remain compatible.
Offline comparisons MUST charge necessary expansions and keep host token accounting distinct.

Source: approved opt-in rollout and offline-first evaluation.

#### Scenario: Compact commands coexist with hook ingress

- **WHEN** new help, completion and collection are used alongside context --host
- **THEN** new commands are discoverable and host notice handling retains the existing protocol.

#### Scenario: Compact task handoff remains opt-in

- **WHEN** task start is prepared with and without --compact
- **THEN** only the opted-in handoff loads the compact reference and preparation creates no task records.

#### Scenario: Offline comparisons preserve required facts

- **WHEN** normal and compact fixtures are compared
- **THEN** required facts and recovered evidence agree and all request/response/expansion overhead is counted without a token-saving claim.
