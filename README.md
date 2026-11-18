# Kitsu

<img src="docs/brand/kitsu-mascot.png" alt="Kitsu’s Pomeranian mascot in a red beanie" width="128" align="right">

Kitsu coordinates coding tasks in **Codex** and **Claude Code**. Ask your agent to plan a feature, review a diff, or fix a bug. Kitsu saves the request, findings, and check results in your Git repository so you can pick up where the last session stopped.

## Install

You need Git and the coding agent you use. Run one command to install the CLI and its matching plugin. The examples use `codex`. Substitute `claude` throughout for Claude Code.

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

Open a new terminal in your project’s Git repository and ask for a plan:

```sh
kitsu task start "Trace how this project handles invalid input and propose one regression test" --until plan --host codex
```

Kitsu prints a task ID and a resume command, then opens your agent. Expect a proposed test you can review before changing any code. In Codex, review Kitsu’s hooks in `/hooks` on first use.

To return later, use the printed resume command. Lost the task ID? Run `kitsu task list`. If startup fails, run `kitsu agent check --host codex`.

<a name="the-roles"></a>

## Choose a workflow

| What you need | What you get |
| --- | --- |
| [Fix a bug from a ticket](docs/task-harness.md#deliver-a-bug-ticket) | A receipt linking the ticket, failing reproduction, repaired commit, and verification result. |
| [Review a committed change](docs/task-harness.md#review-a-committed-change) | Findings tied to the commit range you asked the agent to inspect. |
| [Coordinate specialist roles](docs/workflow.md) | A shared task record for handoffs between architecture, implementation, testing, and review. |

Already in an agent session? Ask Codex to “use Kitsu to review my staged changes,” or enter `/kitsu:task` with your request in Claude Code.

[All guides](docs/README.md) · [CLI reference](docs/cli-reference.md) · [Report a problem](https://github.com/belevtsev/kitsu/issues) · [Contribute](CONTRIBUTING.md) · [License](LICENSE)
