# Agents

Worker agents that a coordinator dispatches for substantial work. Each one gets
a written brief and works from a contract, so it needs no other context, and
each is kept independent of the others' reasoning: the implementer never sees
the design discussion, the test engineer never sees the implementation, and the
reviewer is neither of them.

| Agent | Does | Contract |
|---|---|---|
| [spec-writer](https://github.com/belevtsev/agent-workflows/blob/main/agents/spec-writer.md) | OpenSpec change folder from source documents: proposal, spec deltas with testable scenarios, sized tasks, open questions | [spec-writer](../skills/task-orchestration/references/contracts/spec-writer.md) |
| [spec-reviewer](https://github.com/belevtsev/agent-workflows/blob/main/agents/spec-reviewer.md) | Severity-graded findings on a change folder against its sources and the current code | [spec-reviewer](../skills/task-orchestration/references/contracts/spec-reviewer.md) |
| [architect](https://github.com/belevtsev/agent-workflows/blob/main/agents/architect.md) | Decision record, interface contract with numbered requirements, implementation brief | [architect](../skills/task-orchestration/references/contracts/architect.md) |
| [implementer](https://github.com/belevtsev/agent-workflows/blob/main/agents/implementer.md) | Smallest clean change that meets the contract, built from reusable blocks, with a self-review | [implementer](../skills/task-orchestration/references/contracts/implementer.md) |
| [test-engineer](https://github.com/belevtsev/agent-workflows/blob/main/agents/test-engineer.md) | Test matrix and unit, integration and end-to-end tests from the contract alone | [test-engineer](../skills/task-orchestration/references/contracts/test-engineer.md) |
| [cw-reviewer](https://github.com/belevtsev/agent-workflows/blob/main/global/claude-agents/cw-reviewer.md) | Independent review of the integrated change | [worker handoffs](../skills/task-orchestration/references/worker-handoffs.md) |
| [cw-test-author](https://github.com/belevtsev/agent-workflows/blob/main/global/claude-agents/cw-test-author.md) | Independent tests for a smaller substantial task | [test author](../skills/task-orchestration/references/test-author.md) |
| [cw-github-worker](https://github.com/belevtsev/agent-workflows/blob/main/global/claude-agents/cw-github-worker.md) | Commit, push and pull request from verified facts | [GitHub worker](../skills/task-orchestration/references/github-worker.md) |

Substantial work that builds new behaviour runs seven roles in order: spec
writer → spec reviewer → architect → interface contract → implementer and test
engineer in parallel, isolated from each other → integrate and run the test
matrix → reviewer, then `cw task verify` and a commit through the GitHub worker.
The spec writer turns requirements, tickets and pages into an
[OpenSpec](https://github.com/Fission-AI/OpenSpec) change under
`openspec/changes/<change-id>/` (creating only that folder when a repository has
no `openspec/`), and never invents a requirement: anything the sources leave
open becomes an open question. The spec reviewer checks the folder against the
sources and the code without seeing the writer's reasoning. The spec's scenarios
reach the test engineer as requirements through the architect's interface
contract. The flow, when the spec step is skipped and when each role runs are
in the [contracts overview](../skills/task-orchestration/references/contracts/README.md).
Under the Claude Code plugin the agent types are namespaced, for example
`agent-workflows:architect`; the [host mapping](../skills/task-orchestration/references/hosts.md#worker-agent-types)
lists both forms. Every agent here is listed in
[`.claude-plugin/plugin.json`](https://github.com/belevtsev/agent-workflows/blob/main/.claude-plugin/plugin.json); an agent file that
is not listed there does not load.

Each agent has an eval suite under [`evals/`](https://github.com/belevtsev/agent-workflows/blob/main/evals/README.md).

Agent definitions live in [`agents/`](https://github.com/belevtsev/agent-workflows/tree/main/agents) and [`global/claude-agents/`](https://github.com/belevtsev/agent-workflows/blob/main/global/claude-agents/); keep only agent files in `agents/`, because the plugin loads every Markdown file there as an agent.
