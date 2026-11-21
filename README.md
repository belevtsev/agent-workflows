<p align="center">
  <img src="docs/brand/kitsu-banner.png" width="400" alt="Kitsu. Coding agents, working together. A Pomeranian in a red beanie holds a checked code-change card.">
</p>

# Kitsu

**Coordinate Codex and Claude Code from a ticket to a verified change.**

Give Kitsu an issue, a design, or an unfinished patch. In the configured workflow preview, you choose which host plans, writes tests, implements, and reviews. Kitsu passes work between roles, runs the checks, and keeps the request and evidence under one task ID. Start in either plugin and pick up where you left off in the same Git worktree. [See Codex and Claude Code work on one task](#how-kitsu-works)

Use the task skill in either host to save feature acceptance criteria, verify the exact candidate, and share a Markdown receipt. Start a failed GitHub check repair with `kitsu task ci` and an approved local reproduction. [Deliver a feature](docs/task-harness.md#deliver-a-feature) · [Repair a failed PR check](docs/task-harness.md#repair-a-failed-pr-check)

You can opt in to running eligible feature checks before the first required review. You can also enable one Jev story review at intake for new features in your checkout. [Verify before review](docs/task-harness.md#verify-before-review) · [Jev setup](docs/advisers.md#review-feature-stories-at-intake-with-jev)

## Install

Install the Kitsu CLI and matching plugin for the host you use. On Windows, use Git for Windows. The preview installer lets you choose a default host interactively, or pass the host explicitly as shown below. [Preview installation](docs/release-install.md#install-a-ci-artifact-before-publication)

| Your agent | First time | Use in chat |
| --- | --- | --- |
| **Codex** | [Install for Codex](#install-for-codex) | [Start](#quick-start) · [Resume](#resume-later) · [Prompt examples](#codex-chat-examples) |
| **Claude Code** | [Install for Claude Code](#install-for-claude-code) | [Start](#first-task-in-claude-code) · [Resume](#resume-in-claude-code) · [Prompt examples](#claude-code-chat-examples) |

The release installers follow the latest published tag. For commands listed under **Unreleased**, use a matching CLI and plugin from CI. [Install a CI build](docs/release-install.md#install-a-ci-artifact-before-publication) · [Changelog](CHANGELOG.md#unreleased)

[Latest release](https://github.com/belevtsev/kitsu/releases/latest) · [Pinned and offline installation](docs/release-install.md) · [How Kitsu works](#how-kitsu-works)

## Codex

### Install for Codex

With Git and the `codex` command installed, run one command in your terminal. [Set up Codex CLI](https://developers.openai.com/codex/cli/)

**macOS or Linux**

```sh
(
  installer=$(mktemp) &&
    trap 'rm -f "$installer"' 0 &&
    curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh -o "$installer" &&
    grep -Fq 'to check the installed version.' "$installer" &&
    sh -n "$installer" &&
    sh "$installer" --host codex
)
```

**Windows PowerShell**

```powershell
& ([scriptblock]::Create([Net.WebClient]::new().DownloadString('https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1'))) -AgentHost codex
```

Restart Codex in your project’s Git worktree. On first use, open `/hooks` to review and trust Kitsu’s hooks.

<details>
<summary>Codex can’t find Kitsu</summary>

Run `kitsu agent check --host codex` in a new terminal in the project and follow the setup instructions it prints.

</details>

### Quick start

Paste this into a fresh Codex chat. Save the task ID from the reply.

> Use Kitsu’s task skill to find how this repository runs its tests. Give me the command you found, one test worth adding, and the task ID. Stop after the plan.

> Use Kitsu’s task skill to add `--format=json` to order export. Record criteria for empty output `[]`, quote round-tripping, and unchanged default text. Verify them, then give me the task ID and a shareable receipt. Leave the patch uncommitted.

### Resume later

Open a new Codex chat in the **same Git worktree** and use the ID from the first reply. If you lost it, ask Codex to list your saved Kitsu tasks.

```text
Use Kitsu to resume task <task-id>. Show me the saved test plan and the next step.
```

<a name="start-at-any-stage"></a>

### Codex chat examples

<details>
<summary><a name="bug-fix"></a><strong>Bug fix</strong> · Diagnose a failing test and verify the repair</summary>

> Use Kitsu’s task skill to investigate the failing test. Reproduce the failure, explain the cause, and stop before editing. Give me the task ID.

When you’re ready for the fix, reply in the same chat.

> Fix the cause, add a regression test, and verify the result. Leave the changes uncommitted.

</details>

<details>
<summary><a name="design"></a><strong>Design</strong> · Specify how an export flag should behave</summary>

> Use Kitsu’s task skill. Design a flag for choosing order export columns. Show example commands and expected output for default, selected, and unknown columns. Stop at the proposal.

</details>

<details>
<summary><a name="architecture"></a><strong>Architecture</strong> · Choose how to export a million orders</summary>

> Use Kitsu’s task skill. We need to export a million orders with 256 MiB of memory. Compare a database cursor with bounded pages. Recommend an approach and explain how it handles cancellation.

</details>

<details>
<summary><a name="review"></a><strong>Review</strong> · Find defects in a staged patch</summary>

> Use Kitsu’s task skill. Independently review my staged CSV export patch. Check default columns and partial files after errors. Report each defect with its location and a reproducing test. Stop after review.

</details>

<details>
<summary><a name="implementation"></a><strong>Implementation</strong> · Build and test an export flag</summary>

> Use Kitsu’s task skill. Add `--columns id,total` to the order export. Preserve the specified order and existing defaults. Reject unknown columns before writing. Test all three cases through the CLI.

</details>

<details>
<summary><a name="ticket-intake-verification-and-delivery"></a>Ticket intake, verification, and delivery</summary>

**Ticket intake**

> Use Kitsu’s task skill. Read the issue I linked. Summarize the expected behavior and open questions before editing.

**Verification**

> Use Kitsu’s task skill. Run the exporter on a field containing a comma. Check that the CSV round-trips and report the result. Leave the patch as it is.

**Delivery**

> Use Kitsu’s task skill. Verify my staged export fix. If the checks pass, commit it locally, push this branch, and open a PR for review.

</details>

## Claude Code

### Install for Claude Code

With Git and the `claude` command installed, run one command in your terminal. [Set up Claude Code](https://code.claude.com/docs/en/quickstart)

**macOS or Linux**

```sh
(
  installer=$(mktemp) &&
    trap 'rm -f "$installer"' 0 &&
    curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh -o "$installer" &&
    grep -Fq 'to check the installed version.' "$installer" &&
    sh -n "$installer" &&
    sh "$installer" --host claude
)
```

**Windows PowerShell**

```powershell
& ([scriptblock]::Create([Net.WebClient]::new().DownloadString('https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1'))) -AgentHost claude
```

Open a new terminal in your project’s Git worktree and start `claude`. Open `/plugin` to check that Kitsu is enabled.

<details>
<summary>Claude Code can’t find Kitsu</summary>

Run `kitsu agent check --host claude` in the project’s terminal and follow the setup instructions it prints.

</details>

### First task in Claude Code

Paste this into a fresh Claude Code chat. Save the task ID from the reply.

> /kitsu:task Find how this repository runs its tests. Give me the command you found, one test worth adding, and the task ID. Stop after the plan.

> /kitsu:task Add `--format=json` to order export. Record criteria for empty output `[]`, quote round-tripping, and unchanged default text. Verify them, then give me the task ID and a shareable receipt. Leave the patch uncommitted.

### Resume in Claude Code

Open a new Claude Code chat in the **same Git worktree** and use the ID from the first reply. If you lost it, ask Claude Code to list your saved Kitsu tasks.

```text
/kitsu:task Resume task <task-id>. Show me the saved test plan and the next step.
```

### Claude Code chat examples

<details>
<summary><a name="bug-fix-1"></a><strong>Bug fix</strong> · Diagnose a failing test and verify the repair</summary>

> /kitsu:task Investigate the failing test. Reproduce the failure, explain the cause, and stop before editing. Give me the task ID.

When you’re ready for the fix, reply in the same chat.

> Fix the cause, add a regression test, and verify the result. Leave the changes uncommitted.

</details>

<details>
<summary><a name="design-1"></a><strong>Design</strong> · Specify how an export flag should behave</summary>

> /kitsu:task Design a flag for choosing order export columns. Show example commands and expected output for default, selected, and unknown columns. Stop at the proposal.

</details>

<details>
<summary><a name="architecture-1"></a><strong>Architecture</strong> · Choose how to export a million orders</summary>

> /kitsu:task We need to export a million orders with 256 MiB of memory. Compare a database cursor with bounded pages. Recommend an approach and explain how it handles cancellation.

</details>

<details>
<summary><a name="review-1"></a><strong>Review</strong> · Find defects in a staged patch</summary>

> /kitsu:task Independently review my staged CSV export patch. Check default columns and partial files after errors. Report each defect with its location and a reproducing test. Stop after review.

</details>

<details>
<summary><a name="implementation-1"></a><strong>Implementation</strong> · Build and test an export flag</summary>

> /kitsu:task Add `--columns id,total` to the order export. Preserve the specified order and existing defaults. Reject unknown columns before writing. Test all three cases through the CLI.

</details>

<details>
<summary><a name="ticket-intake-verification-and-delivery-1"></a>Ticket intake, verification, and delivery</summary>

**Ticket intake**

> /kitsu:task Read the issue I linked. Summarize the expected behavior and open questions before editing.

**Verification**

> /kitsu:task Run the exporter on a field containing a comma. Check that the CSV round-trips and report the result. Leave the patch as it is.

**Delivery**

> /kitsu:task Verify my staged export fix. If the checks pass, commit it locally, push this branch, and open a PR for review.

</details>

## Choose who does the work

Role settings and the supervised workflow are opt-in: [add the `kitsu-team` plugin](docs/release-install.md#add-role-coordination) first. It costs several times the tokens of a daily task. Then start the [supervised workflow preview](docs/configured-workflow.md) from either host once a [trusted runner](docs/workflow-runner.md) is registered. For a new task in a repository without role settings, Kitsu uses the default host you chose at install. You can assign each role its own host, model, and reasoning effort.

```sh
kitsu role init
kitsu role set architect --host codex --model gpt-6.1-sol --reasoning max
kitsu role set developer --host claude --model sonnet --reasoning high
kitsu role list
```

`role init` creates `kitsu-roles.toml` in this repository. Omit it when you already have a version 2 roles file. Edit the TOML directly or ask either host to update it.

> Use Kitsu’s managed skill to set the architect to Codex with GPT-6.1 Sol at max reasoning, and the developer to Claude Code with Sonnet at high reasoning. Show me the role table.

In Claude Code, start the same request with `/kitsu-team:managed`. Install and sign in to both hosts if you assign roles to both. Role changes apply to new tasks. [All roles and TOML example](docs/configured-workflow.md#configure-the-roles)

## How Kitsu works

Kitsu gives Codex and Claude Code one task record. Your role settings pick who plans, writes tests, implements, and reviews. The supervised runner sends back signed results from disposable jobs. Kitsu checks write scope before importing edits, keeps the original assertion required, and ties verification to the accepted candidate.

<img src="docs/diagrams/configured-workflow.svg" alt="Kitsu pins role assignments and the intake assertion. The trusted runner runs Codex and Claude Code roles in disposable jobs. Checks and required review return signed results to the same task record." width="100%">

Once the [runner is configured](docs/workflow-runner.md), ask either host to use the supervised workflow. For a terminal start, use `kitsu task run "Fix CSV quoting" --until verify`. Resume the same work with `kitsu task run --resume TASK_ID`.

[See the workflow and its boundaries](docs/workflow.md) · [Follow a bug ticket to its verified commit](docs/task-harness.md#deliver-a-bug-ticket)

<a name="the-roles"></a>

## Guides

- **Daily workflow** · [Task records and delivery receipts](docs/task-harness.md) · [Start from a terminal](docs/cli.md#check-setup-and-start-a-task)
- **Agent coordination** · [Specialist roles and handoffs](docs/workflow.md) · [Choose role models](docs/roles.md) · [Reuse existing skills](docs/skills.md)
- **Reference** · [All guides](docs/README.md) · [CLI commands](docs/cli-reference.md)

[Report a problem](https://github.com/belevtsev/kitsu/issues) · [Contribute](CONTRIBUTING.md) · [License](LICENSE)
