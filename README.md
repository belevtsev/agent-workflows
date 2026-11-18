# Kitsu

<img src="docs/brand/kitsu-mascot.png" alt="Kitsu’s Pomeranian mascot in a red beanie" width="128" align="right">

Kitsu lets you start a coding task in **Codex** or **Claude Code** and pick it up in a fresh chat. Your agent uses the Kitsu CLI to save the request, findings, and check results in your Git worktree.

## Install

Choose **[Codex](#codex)** or **[Claude Code](#claude-code)**. Run the installer once for each agent you use. It downloads the CLI and matching plugin from the latest public release and registers the plugin with that agent. On Windows, use Git for Windows. [Pinned versions and offline installation](docs/release-install.md)

## Codex

### Install for Codex

With Git and the `codex` command installed, run the command for your platform in a terminal. [Set up Codex CLI](https://developers.openai.com/codex/cli/)

**macOS or Linux**

```sh
curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh | sh -s -- --host codex
```

**Windows PowerShell**

```powershell
& ([scriptblock]::Create([Net.WebClient]::new().DownloadString('https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1'))) -AgentHost codex
```

Restart Codex and open your project’s Git worktree. On first use, open `/hooks` to review and trust Kitsu’s hooks. If Kitsu is missing, run `kitsu agent check --host codex` in a new terminal in the project.

### Quick start

Paste this into a fresh Codex chat. Save the task ID from the reply.

> Use Kitsu’s task skill to find how this repository runs its tests. Give me the command you found, one test worth adding, and the task ID. Stop after the plan.

### Resume later

In a new Codex chat in the **same Git worktree**, replace `<task-id>` with the saved ID. If you lost it, ask Codex to list your saved Kitsu tasks.

```text
Use Kitsu to resume task <task-id>. Show me the saved test plan and the next step.
```

<a name="start-at-any-stage"></a>

### Codex chat examples

#### Bug fix

> Use Kitsu’s task skill to investigate the failing test. Reproduce the failure, explain the cause, and stop before editing. Give me the task ID.

When you’re ready for the fix, reply in the same chat.

> Fix the cause, add a regression test, and verify the result. Leave the changes uncommitted.

#### Design

> Use Kitsu’s task skill. Design a flag for choosing order export columns. Show example commands and expected output for default, selected, and unknown columns. Stop at the proposal.

#### Architecture

> Use Kitsu’s task skill. We need to export a million orders with 256 MiB of memory. Compare a database cursor with bounded pages. Recommend an approach and explain how it handles cancellation.

#### Review

> Use Kitsu’s task skill. Independently review my staged CSV export patch. Check default columns and partial files after errors. Report each defect with its location and a reproducing test. Stop after review.

#### Implementation

> Use Kitsu’s task skill. Add `--columns id,total` to the order export. Preserve the specified order and existing defaults. Reject unknown columns before writing. Test all three cases through the CLI.

#### Ticket intake, verification, and delivery

| Task | Chat prompt |
| --- | --- |
| Ticket intake | Use Kitsu’s task skill. Read the issue I linked. Summarize the expected behavior and open questions before editing. |
| Verification | Use Kitsu’s task skill. Run the exporter on a field containing a comma. Check that the CSV round-trips and report the result. Leave the patch as it is. |
| Delivery | Use Kitsu’s task skill. Verify my staged export fix. If the checks pass, commit it locally, push this branch, and open a PR for review. |

## Claude Code

### Install for Claude Code

With Git and the `claude` command installed, run the command for your platform in a terminal. [Set up Claude Code](https://code.claude.com/docs/en/quickstart)

**macOS or Linux**

```sh
curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh | sh -s -- --host claude
```

**Windows PowerShell**

```powershell
& ([scriptblock]::Create([Net.WebClient]::new().DownloadString('https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1'))) -AgentHost claude
```

Open a new terminal in your project’s Git worktree and start `claude`. You can check that Kitsu is enabled in `/plugin`. If it is missing, run `kitsu agent check --host claude` in the project’s terminal.

### First task in Claude Code

Paste this into a fresh Claude Code chat. Save the task ID from the reply.

> /kitsu:task Find how this repository runs its tests. Give me the command you found, one test worth adding, and the task ID. Stop after the plan.

### Resume in Claude Code

In a new Claude Code chat in the **same Git worktree**, replace `<task-id>` with the saved ID. If you lost it, ask Claude Code to list your saved Kitsu tasks.

```text
/kitsu:task Resume task <task-id>. Show me the saved test plan and the next step.
```

### Claude Code chat examples

#### Bug fix

> /kitsu:task Investigate the failing test. Reproduce the failure, explain the cause, and stop before editing. Give me the task ID.

When you’re ready for the fix, reply in the same chat.

> Fix the cause, add a regression test, and verify the result. Leave the changes uncommitted.

#### Design

> /kitsu:task Design a flag for choosing order export columns. Show example commands and expected output for default, selected, and unknown columns. Stop at the proposal.

#### Architecture

> /kitsu:task We need to export a million orders with 256 MiB of memory. Compare a database cursor with bounded pages. Recommend an approach and explain how it handles cancellation.

#### Review

> /kitsu:task Independently review my staged CSV export patch. Check default columns and partial files after errors. Report each defect with its location and a reproducing test. Stop after review.

#### Implementation

> /kitsu:task Add `--columns id,total` to the order export. Preserve the specified order and existing defaults. Reject unknown columns before writing. Test all three cases through the CLI.

#### Ticket intake, verification, and delivery

| Task | Chat prompt |
| --- | --- |
| Ticket intake | /kitsu:task Read the issue I linked. Summarize the expected behavior and open questions before editing. |
| Verification | /kitsu:task Run the exporter on a field containing a comma. Check that the CSV round-trips and report the result. Leave the patch as it is. |
| Delivery | /kitsu:task Verify my staged export fix. If the checks pass, commit it locally, push this branch, and open a PR for review. |

<a name="the-roles"></a>

## Guides

[Daily tasks and delivery receipts](docs/task-harness.md) · [Specialist roles and handoffs](docs/workflow.md) · [Choose role models](docs/roles.md) · [Reuse existing skills](docs/skills.md) · [Start a task from the terminal](docs/cli.md#check-setup-and-start-a-task)

[All guides](docs/README.md) · [CLI reference](docs/cli-reference.md) · [Report a problem](https://github.com/belevtsev/kitsu/issues) · [Contribute](CONTRIBUTING.md) · [License](LICENSE)
