# Agents

Worker agents that a coordinator dispatches for substantial work. Each one gets
a written brief and works from a contract, so it needs no other context, and
each is kept independent of the others' reasoning: the implementer never sees
the design discussion, the test engineer never sees the implementation, and a
reviewer never reviews its own work.

| Agent | Model, effort | Does | Contract |
|---|---|---|---|
| [spec-writer](https://github.com/belevtsev/agent-workflows/blob/main/agents/spec-writer.md) | Opus, high | OpenSpec change folder from source documents: proposal, spec deltas with testable scenarios, sized tasks, open questions | [spec-writer](../skills/task-orchestration/references/contracts/spec-writer.md) |
| [architect](https://github.com/belevtsev/agent-workflows/blob/main/agents/architect.md) | Opus, xhigh | Decision record, interface contract with numbered requirements, implementation brief | [architect](../skills/task-orchestration/references/contracts/architect.md) |
| [implementer](https://github.com/belevtsev/agent-workflows/blob/main/agents/implementer.md) | Opus, high | Smallest clean change that meets the contract, built from reusable blocks, with a self-review | [implementer](../skills/task-orchestration/references/contracts/implementer.md) |
| [test-engineer](https://github.com/belevtsev/agent-workflows/blob/main/agents/test-engineer.md) | Opus, high | Test matrix and unit, integration and end-to-end tests from the contract alone; the one independent test author | [test-engineer](../skills/task-orchestration/references/contracts/test-engineer.md) |
| [reviewer](https://github.com/belevtsev/agent-workflows/blob/main/agents/reviewer.md) | Opus, high | Severity-graded findings at every stage: spec, design, tests (before integration, without the implementation) and code | [spec](../skills/task-orchestration/references/contracts/spec-review.md), [design](../skills/task-orchestration/references/contracts/design-review.md), [test](../skills/task-orchestration/references/contracts/test-review.md) and [code](../skills/task-orchestration/references/contracts/code-review.md) review |
| [github-worker](https://github.com/belevtsev/agent-workflows/blob/main/agents/github-worker.md) | Sonnet, medium | Commit, push and pull request from verified facts | [GitHub worker](../skills/task-orchestration/references/github-worker.md) |

Substantial work that builds new behaviour has a review loop at every stage:
spec writer ⇄ reviewer (spec) → architect ⇄ reviewer (design) → implementer ∥
test engineer ⇄ reviewer (tests) → integrate and run → reviewer (code) ⇄
implementer → `cw task verify` → a commit through the GitHub worker. Findings go
back to the author of that stage, and the review cap applies to each stage. The
spec writer turns requirements, tickets and pages into an
[OpenSpec](https://github.com/Fission-AI/OpenSpec) change under
`openspec/changes/<change-id>/` (creating only that folder when a repository has
no `openspec/`), and never invents a requirement: anything the sources leave
open becomes an open question. Advisers, when available, join only at the
decision points the
[consultation reference](../skills/task-orchestration/references/consultation-helper.md#adviser-points-in-the-flow)
lists. The flow, when a stage is skipped and when each role runs are in the
[contracts overview](../skills/task-orchestration/references/contracts/README.md).

Under the Claude Code plugin the agent types are namespaced, for example
`agent-workflows:reviewer`; the [host mapping](../skills/task-orchestration/references/hosts.md#worker-agent-types)
maps each policy role to its type. The frontmatter model is a default: the
coordinator passes the role's model and effort on every dispatch. Every agent
here is listed in
[`.claude-plugin/plugin.json`](https://github.com/belevtsev/agent-workflows/blob/main/.claude-plugin/plugin.json); an agent file that
is not listed there does not load.

The spec writer, architect, implementer, test engineer and reviewer have eval
suites under [`evals/`](https://github.com/belevtsev/agent-workflows/blob/main/evals/README.md);
the reviewer's covers spec, design and test review.

Agent definitions live in [`agents/`](https://github.com/belevtsev/agent-workflows/tree/main/agents); keep only agent files there, because the plugin loads every Markdown file there as an agent.
