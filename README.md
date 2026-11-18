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

Open a fresh chat in your project. Paste the example for your agent.

**Codex** — On first use, open `/hooks` to review and trust Kitsu’s hooks.

> Use Kitsu’s task skill to investigate the failing test. Reproduce the failure, explain the cause, and stop before editing. Give me the task ID.

**Claude Code**

> /kitsu:task Investigate the failing test. Reproduce the failure, explain the cause, and stop before editing. Give me the task ID.

When you’re ready for the fix, reply in the same chat with either agent:

> Fix the cause, add a regression test, and verify the result. Leave the changes uncommitted.

### Resume later

Open a fresh chat in the **same Git worktree**. Replace `<task-id>` with the ID your agent gave you.

- **Codex:** `Use Kitsu to resume task <task-id> and show me the next step.`
- **Claude Code:** `/kitsu:task Resume task <task-id> and show me the next step.`

Lost the ID? Ask your agent to list your saved Kitsu tasks.

<a name="the-roles"></a>

## Start at any stage

Pick the row that matches your work. In Codex, start your message with “Use Kitsu’s task skill.” In Claude Code, start it with `/kitsu:task`.

| Starting point | Chat prompt |
| --- | --- |
| Ticket | Read the issue I linked. Summarize the expected behavior and what we still need to learn. Stop before editing. |
| Architecture | Compare streaming and buffering for our CSV export. Recommend one and explain the trade-off. Stop at the design. |
| Implementation | Implement the approved streaming design. Add a test for an empty export and leave the changes uncommitted. |
| Review | Independently review my staged CSV export patch. Report findings with file locations and stop before editing. |
| Verification | Run the exporter on a field containing a comma. Check that the CSV round-trips and report the result. Leave the patch as it is. |
| Delivery | Verify my staged export fix. If the checks pass, commit it locally, push this branch, and open a PR for review. |

[Bug tickets and delivery receipts](docs/task-harness.md#deliver-a-bug-ticket) · [Specialist roles and handoffs](docs/workflow.md)

Prefer to launch the agent from your terminal? Use [`kitsu task start`](docs/cli.md#check-setup-and-start-a-task).

[All guides](docs/README.md) · [CLI reference](docs/cli-reference.md) · [Report a problem](https://github.com/belevtsev/kitsu/issues) · [Contribute](CONTRIBUTING.md) · [License](LICENSE)
