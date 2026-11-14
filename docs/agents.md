# Agents

Worker agents that a coordinator dispatches for substantial work. Each one gets
a written brief and works from a contract, so it needs no other context, and
each is kept independent of the others' reasoning: the implementer never sees
the design discussion, the test engineer never sees the implementation, and a
reviewer never reviews its own work.

| Agent | Model, effort | Does | Contract |
|---|---|---|---|
| [spec-writer](https://github.com/belevtsev/agent-workflows/blob/main/agents/spec-writer.md) | `opus`, high | OpenSpec change folder from source documents: proposal, spec deltas with testable scenarios, sized tasks, open questions | [spec-writer](../skills/task-orchestration/references/contracts/spec-writer.md) |
| [architect](https://github.com/belevtsev/agent-workflows/blob/main/agents/architect.md) | `opus`, xhigh | Decision record, interface contract with numbered requirements, implementation brief; an ADR in the repository's ADR folder for a costly-to-reverse choice | [architect](../skills/task-orchestration/references/contracts/architect.md) |
| [implementer](https://github.com/belevtsev/agent-workflows/blob/main/agents/implementer.md) | `opus`, high | Smallest clean change that meets the contract, built from reusable blocks, with a self-review | [implementer](../skills/task-orchestration/references/contracts/implementer.md) |
| [test-engineer](https://github.com/belevtsev/agent-workflows/blob/main/agents/test-engineer.md) | `opus`, high | Test matrix and unit, integration and end-to-end tests from the contract alone; the one independent test author | [test-engineer](../skills/task-orchestration/references/contracts/test-engineer.md) |
| [adversarial-tester](https://github.com/belevtsev/agent-workflows/blob/main/agents/adversarial-tester.md) | `opus`, high | Opt-in, beside code review: white-box tests that must fail on the integrated source, each a finding for the implementer; kept as source-access regression tests, never black-box coverage | [adversarial-tester](../skills/task-orchestration/references/contracts/adversarial-tester.md) |
| [reviewer](https://github.com/belevtsev/agent-workflows/blob/main/agents/reviewer.md) | `opus`, high | Severity-graded findings at every stage: spec, design, tests (before integration, without the implementation) and code | [spec](../skills/task-orchestration/references/contracts/spec-review.md), [design](../skills/task-orchestration/references/contracts/design-review.md), [test](../skills/task-orchestration/references/contracts/test-review.md) and [code](../skills/task-orchestration/references/contracts/code-review.md) review |
| [github-worker](https://github.com/belevtsev/agent-workflows/blob/main/agents/github-worker.md) | `sonnet`, medium | Commit, push and pull request from verified facts, when `cw publish` does not fit (the fallback) | [GitHub worker](../skills/task-orchestration/references/github-worker-fallback.md) |

A substantial task runs the `lite` profile by default: implementer ∥ test
engineer ⇄ reviewer (tests) → integrate and run → reviewer (code) ⇄
implementer → `cw task verify`. `standard` adds architect ⇄ reviewer (design)
first, for an open design choice or a public interface change; `strict` also
adds spec writer ⇄ reviewer (spec), when the user asks or the change touches
security, data integrity or concurrency
([profiles](operations.md#profiles)). Every review loops back to its stage's
author. The task ends with a commit with `cw publish`, run by the
coordinator under the user's authorization (the GitHub worker for the fallback
cases). Findings go
back to the author of that stage, and the review cap applies to each stage. The
spec writer turns requirements, tickets and pages into an
[OpenSpec](https://github.com/Fission-AI/OpenSpec) change under
`openspec/changes/<change-id>/` (creating only that folder when a repository has
no `openspec/`), and never invents a requirement: anything the sources leave
open becomes an open question. Advisers, when available, join only at the
decision points the
[adviser guidance](../skills/task-orchestration/references/adviser-providers.md#adviser-points-in-the-flow)
lists. The flow, when a stage is skipped and when each role runs are in the
[contracts overview](../skills/task-orchestration/references/contracts/README.md).

Under the Claude Code plugin the agent types are namespaced, for example
`agent-workflows:reviewer`; the [host mapping](../skills/task-orchestration/references/hosts.md)
maps each policy role to its type. The frontmatter model is a default: the
coordinator passes the role's model and effort on every dispatch. Every agent
sets `disallowedTools: Skill, Agent, Workflow`: under Claude Code a worker
cannot load a skill by name, dispatch subagents of its own or run a workflow
script, so only the coordinator dispatches. The reviewer and the test engineer
also set `omitClaudeMd: true`, so under Claude Code they start without the
user's and the project's CLAUDE.md files; their contracts and briefs carry the
project rules they apply. The Claude Code version in the [host requirements](operations.md#host-requirements) honours it; an older
client ignores it without a warning. Codex has no such switch.

Neither agent sets `isolation: worktree`, although Claude Code honours it for
plugin agents, because it would not enforce their rules and would break the
flow ([subagent frontmatter](https://code.claude.com/docs/en/sub-agents#supported-frontmatter-fields),
[worktree isolation](https://code.claude.com/docs/en/worktrees#how-claude-code-enforces-isolation)).
The worktree is a fresh checkout of tracked files from the remote default
branch (or `HEAD` under `worktree.baseRef: "head"`), so it lacks the
uncommitted change that code review reads: a reviewer there would review and
test the wrong source. Isolation blocks
edits and git commands aimed at the main checkout, but not reads, so the test
engineer could still read implementation source, in its worktree or by
absolute path. And frontmatter isolation is ignored when agent teams launch the
agent as a teammate. "Never edit what you review" and "never read
implementation source" therefore rest on the contracts, the briefs and the
coordinator's check of the transcript
([dispatching the test engineer](../skills/task-orchestration/references/dispatching-reviews-and-tests.md#dispatch-a-test-engineer)).

Every agent
here is listed in
[`.claude-plugin/plugin.json`](https://github.com/belevtsev/agent-workflows/blob/main/.claude-plugin/plugin.json); an agent file that
is not listed there does not load.

The spec writer, architect, implementer, test engineer, adversarial tester and
reviewer have eval suites under [`evals/`](https://github.com/belevtsev/agent-workflows/blob/main/evals/README.md);
the reviewer's covers spec, design and test review.

Agent definitions live in [`agents/`](https://github.com/belevtsev/agent-workflows/tree/main/agents); keep only agent files there, because the plugin loads every Markdown file there as an agent.

## Design choices

Short answers to alternatives that come up often, with the reason behind each
choice.

**Why not merge the spec writer and the architect?** The spec is settled, and
reviewed against its sources, before any design starts. Keeping the authors
apart is meant to stop the requirements from being bent to fit a design the
author already has in mind. The spec stage runs only under the `strict`
profile, and is skipped there for a bug fix, a refactor or a test-only task,
when a reviewed spec already exists, or when the request is precise enough to
be its own spec
([contracts overview](../skills/task-orchestration/references/contracts/README.md#flow)).

**Why does the test engineer never see the code?** So that the tests cannot
simply mirror the code and pass on the same bug. The test engineer works from
the interface contract and the public interface extract, and takes expected
values from the contract
([quality bar](../skills/task-orchestration/references/contracts/test-engineer.md#quality-bar)).
Its test matrix covers boundary, failure, concurrency, idempotency and security
cases, or says why one does not apply
([required outputs](../skills/task-orchestration/references/contracts/test-engineer.md#required-outputs)).
Test review runs the tests against a stub first, so a test of non-trivial
behaviour that passes against the stub is flagged
([test review](../skills/task-orchestration/references/contracts/test-review.md)).
Code review, which does read the code, also flags a test that could not fail
for the defect it claims to prevent. An internal branch that no requirement
names and that leaves an unexecuted changed block is classified there: when
the final check wrote a coverage profile, code review gives every unexecuted
changed block one class, an untested
requirement (sent to a fresh test engineer by requirement ID, contract-level
inputs and expected outcome, never coverage), behaviour no requirement asks
for, or an unreachable defence
([uncovered changed blocks](../skills/task-orchestration/references/contracts/code-review.md#uncovered-changed-blocks)).
The test engineer itself still never sees coverage.
For a change that touches concurrency, parsing or untrusted input, security or
data integrity, the plan can also opt in to the
[adversarial tester](../skills/task-orchestration/references/contracts/adversarial-tester.md),
which reads the code but keeps only tests that fail on it; those never count as
black-box coverage, and the test engineer never sees them.

**Who defines a finding's severity?** Each review contract and the adversarial
tester's contract grade a finding `blocker`, `major` or `minor`. When writing
a contract: a contract may define a grade for its artefact, never add one.

**Why doesn't the implementer write its own tests?** Code tested only by its
author is one of the failures this project exists to prevent: the same
reasoning that wrote a bug can write a test that misses it. The implementer
proves its change with the checks the brief names and writes a self-review; the
behavioural tests come from the test engineer
([implementer](../skills/task-orchestration/references/contracts/implementer.md#purpose)).
A task that is not [substantial](../global/AGENTS.md#substantial-tasks) is the
exception: the agent writes the tests itself and checks its diff against the
Code Review Rules.

**Does the reviewer ever read the code?** Yes. Code review runs on the
integrated source after the tests have run. It has the whole repository, a
bundle that lists the files changed since the task started, the interface
contract, `design.md`, the test engineer's matrix and requirement map, and the
final check's coverage profile when one exists; it checks the change against the requirements
and the design, and backs each behaviour finding with a test that fails on the
current source
([code review](../skills/task-orchestration/references/contracts/code-review.md#inputs-you-receive)).
Only test review never sees the implementation, so it judges the tests by the
contract alone.

**Why does a contract change go back to the architect, without the code?** A
worker reads only the contract its brief names, so each role's output is
independent evidence. An amendment keeps that: the architect and the re-bind
reviewer read source only in a separate checkout of what the design was read
from, so code cannot shape the contract or reach the test engineer through
it. The implementer's first refreshed assignment only puts the amended
declarations into source, and the rest of its work waits for the test
engineer's refreshed assignment, so no implementer edit lands between the
extract and `cw task assign`
([contract amendment](../skills/task-orchestration/references/contract-amendment.md)).

**Why not run more agents on a cheaper model?** This project has not measured
whether a cheaper model keeps the same review and test quality, or how much it
would save; the agents under test in its eval suites run on `opus` by default
([evals](https://github.com/belevtsev/agent-workflows/blob/main/evals/README.md)).
On Claude Code the GitHub worker already runs on `sonnet` at medium effort,
and every later review of a stage (a diff-scoped follow-up, a review scoped to
a fix, a confirmation pass) runs on `sonnet` at high. Read-only evidence work
runs cheaper too: summarization on `sonnet` and lookup on `haiku`, both at
high. The other roles run on `opus`
([Claude Code policy](../skills/task-orchestration/model-policy.claude.yaml));
whether the implementer, test review and spec review keep their quality on
`sonnet` is for the evals to show, not a default. Codex roles follow
[model-policy.yaml](../skills/task-orchestration/model-policy.yaml): the `decision` profile at
`max` for planning, architecture and diagnosis, and `sol` at `high` for the other
substantive roles. Moving more roles to a cheaper model is the owner's decision.

**Why is the GitHub worker an agent rather than a shell script?** Routine
publication no longer needs either: it runs as the Go command `cw publish`,
which applies the verify gate itself, so it holds where the commit hook, which
sees only shell commands that name git, cannot look, and under Codex before its
hooks are trusted ([hooks](hooks.md#codex)). First-party automation must be written in Go anyway; the
repository check rejects `.sh` and `.py` files outside `skills/` and
`third_party/` ([contributing](../CONTRIBUTING.md#go-only-automation)). The
worker remains an agent for the fallback cases the command refuses, such as
hunk-level scopes, fork pull requests and content conversions
([publication](../skills/task-orchestration/references/github-worker-fallback.md)).

**Why not run design review and test review in parallel?** Under `standard`
and `strict` the tests are written from the fixed interface contract, and the
contract is fixed only after the design review passes, so test review has
nothing to review until then
([contracts overview](../skills/task-orchestration/references/contracts/README.md#flow)).
What does run in parallel is the implementer and the test engineer, from the
same contract; test review then runs before integration.
