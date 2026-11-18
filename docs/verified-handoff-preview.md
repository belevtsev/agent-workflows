# Verify the handoff preview

Start with a partially working patch. Save its failing behavior in one provider, then resume in the other provider in the same Git worktree. Deliver one local commit, backed by an independent review and a receipt that names the exact delivered content.

## Prepare one candidate

Use the CLI and plugin from the same preview build. Record its Git revision and the CLI archive and plugin checksums. Start from a clean host profile for the installation check, then open both hosts in the intended worktree. Record OS, architecture and host versions. [Install a preview bundle](release-install.md#install-a-ci-artifact-before-publication)

```sh
kitsu version
kitsu agent check --host codex
kitsu agent check --host claude
```

Sign in to both native hosts with an account that can start an independent reviewer. Confirm that both loaded the Kitsu task skill and hook definitions in the live session. In Codex, review and trust the Kitsu definitions in `/hooks`. In Claude Code, check that the plugin is enabled and the Kitsu hooks appear in `/hooks`. The CLI diagnostics check local setup. Record live skill selection and a hook observation separately. If the repository declares checks, inspect them with `kitsu check list` and approve the declaration in your own terminal with `kitsu check trust`. Keep compact mode off for this preview. [Plugin hook setup](hooks.md#verify-plugin-hook-setup)

Choose a real partial fix with a known failing public behavior and a passing adjacent case. Prefer a scoped change whose behavior can be checked locally in minutes. Write down the starting commit and branch, intended paths, and any unrelated work that must be preserved. Prepare equivalent copies in separate worktrees for the two directions. Both hosts within each direction use that direction’s original worktree.

If you need a control patch, prepare the existing CSV quoting fixture from the Kitsu source checkout.

```sh
fixture_root="$(mktemp -d)"
python3 -B docs/benchmarks/harness-request-decisions.py prepare csv-finish-faulty "$fixture_root/codex-to-claude"
python3 -B docs/benchmarks/harness-request-decisions.py prepare csv-finish-faulty "$fixture_root/claude-to-codex"
```

Open the `repo` directory in each copy. The staged `export.py` passes `python3 -B check_csv.py unit` and fails `python3 -B check_csv.py worktree` on a field containing a double quote. Preserve `scratch/session.txt` and keep the grader and starting evidence outside the agent's inputs. [Fixture and grading criteria](benchmarks/harness-request-decisions.md#verify-only-authority-and-delegated-repair-on-the-same-csv-patch)

## Run both native handoffs

| Direction | Start | Resume and review |
| --- | --- | --- |
| Codex → Claude Code | Select Kitsu’s task skill in Codex and use the first prompt. | Run the Claude Code resume prompt. Observe a separate Claude Code reviewer. |
| Claude Code → Codex | Use the first prompt with `/kitsu:task` at the beginning. | Select Kitsu’s task skill in Codex and use the Codex resume prompt. Observe a separate Codex reviewer. |

Replace the placeholders and use the same inputs in each direction. If the starting point came from a ticket, supply its approved Markdown snapshot or stable source reference.

```text
Use Kitsu's task skill on my partial fix for <ticket or behavior>. Read my patch and record a command that demonstrates <remaining failure>. The task is to repair that behavior, get an independent review, verify the result and make one local commit including only <paths>. Preserve my other edits. For this session, stop after saving the failing reproduction and give me the task ID.
```

Before switching, confirm that the task recorded the failure against the original candidate and that the branch, staged content and working edits stayed intact. End the first chat. Open a fresh chat in the same worktree with the other host. Supply the task ID from the first chat.

**Claude Code**

```text
/kitsu:task Resume task <task-id> in this worktree. Recover the saved failure, finish the authorized repair, use an independent Claude Code reviewer, and verify and commit the approved paths locally. Export the delivery receipt.
```

**Codex**

```text
Use Kitsu's task skill to resume task <task-id> in this worktree. Recover the saved failure, finish the authorized repair, use an independent Codex reviewer, and verify and commit the approved paths locally. Export the delivery receipt.
```

At the end, replace `preview-csv` with the saved ID, inspect the task and export the receipt locally.

```sh
task_id=preview-csv
kitsu task next "$task_id" --brief
kitsu task receipt "$task_id" --markdown
git rev-parse HEAD 'HEAD^{tree}'
git show --format=fuller --stat HEAD
```

The receipt must identify the actual commit and checked tree. Compare its behavior result, check results and independent review with the task record. For an imported bug ticket, also check the source and replayed-on-commit proof in the saved ticket receipt. Rerun the public behavior and a known-good case on the actual commit. Verify that its parent and touched paths match the authorization, then check that unrelated work is still intact. A second fresh resume and receipt export must return the same delivery without executing checks or creating another commit. Keep delivery local.

## Capture enough evidence to decide

Keep detailed local evidence with the developer's permission. Share only a redacted row and the approved receipt. Remove credentials, absolute paths, raw transcripts, task prose and private ticket or repository details. Use a tester alias and stable short references for local records. Confirm that the receipt can be shared with its intended audience before sending it.

| Field | Record |
| --- | --- |
| Candidate and setup | Build revision, archive and plugin checksums, OS and host versions, setup and live hook outcomes. |
| Handoff | Tester alias, direction, task reference, checkpoint, resume outcome, and whether another task or worker was created. |
| Review and verification | Requested review scope, independence observed in the receiving host, recorded reviewer result, failure and pass outcomes, check declaration identity and verdicts. Mark requested model/effort separately from actual identity if reported by the host. |
| Git and receipt | Base, delivered commit and checked tree for repositories that can share them. Record receipt match, commit count, path authorization, preserved edits, and protected behavior check outcomes. |
| Time and friction | Setup and task wall time, deliberate pause duration, user intervention, failed commands and retries. Record non-default flags, configured limits and the stop reason when they affect the run. |

Use a stopwatch from the first task prompt to the first successful export. Keep setup and the deliberate interruption interval separate. Include any retry or recovery in the attempt record. Mark unavailable timing or usage as unknown. Use these observations to identify friction. A paid cost comparison requires separate approval.

## Offline check and promotion

Run the existing checks against the final integrated candidate before asking developers to try it.

```sh
GOWORK=off go test -count=1 ./scenarios/harness ./internal/cli ./scenarios/plugin
GOWORK=off go test -count=1 ./internal/repocheck
```

The offline suite exercises a fresh coordinator on a partial patch, both providers’ review settings, verify-only failure, ticket-linked proof and same-worktree resume. The integrated suite should also exercise pinned check policy, approval, durable delivery recovery and receipt export. Use the exact CI archives for packaged installer validation. The `verify_native_hosts` CI input requires native install evidence. Use the two live handoffs to establish actual provider dispatch, review and resume. [Existing offline controls](benchmarks/harness-request-decisions.md) · [Artifact setup](release-install.md#install-a-ci-artifact-before-publication)

With passing checks and no independent review requirement, the real CLI regression delivers a staged fix in four Kitsu calls. Open the task, record intake, test and commit with the returned ticket. The final reply confirms delivery. Exporting Markdown is a separate read when the developer wants a shareable receipt.

Native acceptance remains pending until the integrated artifact and both signed-in hosts with reviewer access are available, both directions pass, and five developers independently try the documented workflow. Include both directions in the tester group and count each developer once. At least four of five must reach an exact shareable receipt without a maintainer taking over or supplying commands beyond this guide. Record correct permission stops separately from successful deliveries. Investigate any incorrect approval, changed unrelated work, duplicate commit or receipt mismatch before promotion, even if four developers finished.
