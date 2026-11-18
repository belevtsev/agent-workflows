# Kitsu

<img src="docs/brand/kitsu-mascot.png" alt="Kitsu’s Pomeranian mascot in a red beanie" width="128" align="right">

Kitsu guides **Codex and Claude Code** through a coding task in your Git repository. Give your agent a bug, a feature request, or a diff and tell it where to stop. Kitsu keeps the task record so you can resume in a fresh session and see which checks actually passed.

## Install

Install Git and the coding agent you use. The daily workflow is available in the preview bundle. The published v0.0.1 release predates these commands.

1. Open [Kitsu builds on main](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml?query=branch%3Amain+event%3Apush), select a recent run with a successful **Release archives** job, and download **kitsu-archives** under **Artifacts**. Sign in to GitHub to download it.
2. Unzip the download and open a terminal in the extracted folder. Run the command for your platform below. Use `claude` in place of `codex` for Claude Code.

**macOS or Linux**

```sh
sh ./install.sh --archive-dir . --host codex
```

**Windows PowerShell**

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -ArchiveDir . -AgentHost codex
```

This installs the CLI and registers its matching plugin. [Download and install entirely from the terminal](docs/release-install.md#download-and-install-from-the-terminal)

## Quick start

Open a new terminal in your project’s Git repository. Try a read-only task that works with a clean checkout:

```sh
kitsu task start "Find how this project runs its tests and propose one missing smoke test" --until plan --host codex
```

Kitsu opens your agent with the request. In Codex, review and trust the plugin hooks in `/hooks`. For Claude Code, use `--host claude`. If startup needs attention, run `kitsu agent check` with the same host flag.

Already in an agent session? Ask Codex to “Use Kitsu to review my staged changes,” or type `/kitsu:task` with your request in Claude Code.

## Fix a bug from a ticket

Start with a Markdown ticket or public URL. This workflow authorizes a local repair and commit. Private tickets can come from your host’s MCP server or plugin. [Ticket guide](docs/task-harness.md#deliver-a-bug-ticket)

```sh
kitsu task bug csv-fix --from ./tickets/csv.md
kitsu task start --resume csv-fix --host codex
```

After delivery, `kitsu task receipt csv-fix` shows the source ticket, failing reproduction, and passing check on the repaired commit. After an interruption, `kitsu task list` shows your task IDs and `kitsu task next csv-fix` shows where to continue.

## The roles

For larger changes, the [managed workflow](docs/workflow.md) coordinates specialists for lookup, specification, architecture, tests, implementation, review, and Git. You can [choose models per role](docs/roles.md) and [reuse your existing skills](docs/skills.md).

[Daily workflow guide](docs/task-harness.md) · [CLI reference](docs/cli-reference.md) · [Git gate setup](docs/quickstart.md) · [All documentation](docs/README.md) · [MIT license](LICENSE)
