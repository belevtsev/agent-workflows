# Skills

Each folder is one skill: a `SKILL.md` with `name` and `description`
frontmatter, plus optional `references/` the skill loads only when a step needs
them. The agent sees every **automatic** skill's description in each session and
loads the skill when the task matches; an **on request** skill is hidden from that
listing (`disable-model-invocation: true`) and runs only when you name it, for
example `/agent-workflows:principal-engineer`.

Start with `task-orchestration` for substantial changes and `code-review` for
reviews; the rest are specialists those two point to.

| Skill | Loaded | Purpose |
|---|---|---|
| [backend-security-review](backend-security-review/SKILL.md) | automatic | Review backend attack paths and dependency risks with source evidence; use code-review for ordinary correctness. |
| [clocks-and-time](clocks-and-time/SKILL.md) | automatic | Handle durations, timestamps, time zones, DST, and event ordering in Go without clock, skew, or calendar bugs. |
| [code-review](code-review/SKILL.md) | automatic | Review PRs or local diffs for concrete defects, tracing changed behavior with exact evidence and limits. |
| [codebase-design](codebase-design/SKILL.md) | automatic | Design or deepen module interfaces, choosing seams and removing shallow layers so behavior stays testable. |
| [domain-modeling](domain-modeling/SKILL.md) | automatic | Sharpen a project's domain language with a glossary and scenarios; record only costly-to-reverse decisions. |
| [expand-contract](expand-contract/SKILL.md) | automatic | Plan zero-downtime schema, API, and event changes as expand, migrate, and contract steps for rolling deploys. |
| [go-pki-mtls](go-pki-mtls/SKILL.md) | automatic | Design or review Go enrollment, certificate identity, mTLS authorization, and credential or trust rotation. |
| [go-principal-engineer](go-principal-engineer/SKILL.md) | automatic | Resolve consequential Go ownership, lifecycle, durability, or rollout decisions with failure behavior and checks. |
| [graphify](graphify/SKILL.md) | automatic | Explore tracked Go source with an optional local structural graph of package relationships and related symbols. |
| [isolation-anomalies](isolation-anomalies/SKILL.md) | automatic | Find and fix concurrency anomalies such as write skew and lost updates from transaction isolation levels. |
| [observability](observability/SKILL.md) | on request | Language-neutral principles for actionable errors, logs, and metrics at boundaries, with privacy and without noise. |
| [parse-dont-validate](parse-dont-validate/SKILL.md) | automatic | Move checks to input boundaries by returning narrower Go types so invalid states stop spreading. |
| [performance](performance/SKILL.md) | on request | Language-neutral performance practice to measure before optimizing, avoid hot-path hazards, and compare before/after evidence. |
| [principal-engineer](principal-engineer/SKILL.md) | on request | Language-neutral judgment for consequential changes, covering failure modes, ownership and lifecycle, durability, rollout, rollback, and compatibility. |
| [production-plan](production-plan/SKILL.md) | automatic | Create implementation plans for production software changes, grounded in active decisions, project evidence, failure behavior, and verification. |
| [protobuf-contracts](protobuf-contracts/SKILL.md) | automatic | Evolve or review protobuf schemas and generated peers across wire, source API, and ProtoJSON compatibility. |
| [retry-amplification](retry-amplification/SKILL.md) | automatic | Prevent retry storms and metastable failure with retry budgets, single-layer retries, jitter, and load shedding. |
| [security-threat-model](security-threat-model/SKILL.md) | automatic | Build a requested repository threat model with source-backed boundaries, abuse paths, and mitigations. |
| [shell-portability](shell-portability/SKILL.md) | automatic | Write, review, or run shell scripts and installers that must work under sh, dash, busybox, macOS bash 3.2, bash 5, and curl | sh. |
| [software-architecture](software-architecture/SKILL.md) | automatic | Design or assess component ownership, dependencies, and interface evolution across packages or repositories. |
| [task-orchestration](task-orchestration/SKILL.md) | automatic | Coordinate substantial work with bounded workers; dispatch independent test engineers, reviewers at every stage and routine GitHub workers. |
| [test-strategy](test-strategy/SKILL.md) | automatic | Choose tests for changed behavior, including Go fuzzing, with meaningful oracles and verification gates. |

Vendored packs (the Go specialist pack, archify, typesafe-ai) live in
[`third_party/`](../third_party/README.md). To add or change a skill, see
[CONTRIBUTING](../CONTRIBUTING.md#proposing-or-changing-a-skill). This table is
checked against each `SKILL.md` by `catalog_test.go` at the repository root.
