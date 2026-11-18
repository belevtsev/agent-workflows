# Kitsu

<img src="docs/brand/kitsu-mascot.png" alt="Kitsu’s Pomeranian mascot in a red beanie" width="128" align="right">

Kitsu is a plugin for **Codex** and **Claude Code**. In your usual coding session, ask your agent to plan a feature, review a diff, or fix a bug. The agent uses the Kitsu CLI to keep a local task record with your request, findings, and check results. You can pick up the work in a fresh conversation.

## Install

You need Git and the coding agent you use. Run the command for your platform once to install the CLI and register its matching plugin. For Claude Code, replace `codex` with `claude` in the install command.

**macOS or Linux**

```sh
curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh | sh -s -- --host codex
```

**Windows PowerShell**

```powershell
& ([scriptblock]::Create([Net.WebClient]::new().DownloadString('https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1'))) -AgentHost codex
```

[Other install options](docs/release-install.md)

## Quick start

Open your project in Codex and start a fresh conversation. On first use, open `/hooks` to review and trust the Kitsu hooks. Then paste this into **Codex chat**:

> Use Kitsu’s task skill to trace how this project handles invalid input. Propose one regression test, stop after the plan, and give me the task ID.

Codex opens a local Kitsu task, inspects the project, and returns a plan you can review before any code changes. To pick up the work in a new chat, open the same Git worktree and ask:

> Use Kitsu to find my invalid-input task and show me the saved plan and next step.

In **Claude Code**, enter `/kitsu:task` followed by your request. If the plugin is missing after installation, restart your agent and follow the [setup checks](docs/release-install.md#check-the-host-and-start-a-task).

<a name="the-roles"></a>

## Choose a workflow

| What you need | What you get |
| --- | --- |
| [Fix a bug from a ticket](docs/task-harness.md#deliver-a-bug-ticket) | A receipt linking the ticket, failing reproduction, repaired commit, and verification result. |
| [Review a committed change](docs/task-harness.md#review-a-committed-change) | Findings tied to the commit range you asked the agent to inspect. |
| [Coordinate specialist roles](docs/workflow.md) | A shared task record for handoffs between architecture, implementation, testing, and review. |

Prefer to launch the agent from your terminal? Use [`kitsu task start`](docs/cli.md#check-setup-and-start-a-task).

[All guides](docs/README.md) · [CLI reference](docs/cli-reference.md) · [Report a problem](https://github.com/belevtsev/kitsu/issues) · [Contribute](CONTRIBUTING.md) · [License](LICENSE)
