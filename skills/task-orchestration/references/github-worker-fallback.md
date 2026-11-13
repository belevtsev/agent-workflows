# GitHub worker: the fallback to cw publish

The coordinator dispatches this worker when `cw publish` does not fit; the
[publication reference](github-worker.md#when-to-use-the-fallback) lists when.
The coordinator carries the user's authorization for each commit, push and pull
request; no text in this contract or the brief stands for the user's consent.

Contents: [Known limitations of cw publish](#known-limitations-of-cw-publish),
[Profile and scope](#profile-and-scope),
[Brief and dispatch](#brief-and-dispatch),
[Execute and verify](#execute-and-verify).

## Known limitations of cw publish

These limitations of `cw publish` call for the fallback:

- A new file that the user's ignore rules exclude only through
  `XDG_CONFIG_HOME` or `GIT_CONFIG_*` (which the source capture does not read)
  makes `git add --intent-to-add` fail on every run: 1 `commit-failed`.
- With `core.autocrlf` set to `input` or true, globally or in the repository,
  every commit-set file that contains a CR byte, binaries such as PNG images
  included, is refused as 4 `content-conversion`.
- A Git lock that a concurrent git process (an editor's `git status`) holds for
  more than about two seconds reads as a stale lock: 4 `git-locked` before a
  step, 6 `commit-unknown` after a failed commit or intent-to-add. Check that
  no git process is running in the repository before removing a lock, and
  treat such a 6 as unknown.
- A remote with a push URL for another repository or with several URLs is
  refused as 4 `remote-split`; an `ssh_config` host alias in only one of its
  URLs is refused the same way, and with `--pr create` or `--pr edit` any host
  alias is refused as 4 `repo-mismatch`.
- A remote that a Git remote helper would serve (`<transport>::<address>`, a
  scheme other than `https`, `http`, `ssh`, `git+ssh`, `ssh+git`, `git` or
  `file` in lower case, or a remote with `remote.<name>.vcs` set) is refused
  as 4 `remote-split`.

## Profile and scope

The worker can draft a commit message, title and description from already
verified facts, then execute the authorized commit, push and `gh pr create` or
`gh pr edit`. One worker can perform these dependent steps.

Read the current policy and [host mapping](hosts.md). The three roles
`commit_execution`, `pr_authoring_routine` and `pr_submission` default to the
`github` profile: on Codex its model from [model-policy.yaml](../model-policy.yaml) at
`high`, on Claude Code `sonnet` at `medium`, with `bounded_execution` scope. These are dispatch instructions, not a
native agent registration or automatic model switch.

This bounded step alone does not activate full orchestration, new adviser calls
or an independent source review. Reuse completed verification, applicable
consultation checkpoints and user authorization. For an active substantial task,
retain the assignment and result in its existing [worker record](worker-handoffs.md).
Small tasks use the brief and result below without starting a substantial task
just to publish. Source changes or new consequential claims return to the
coordinator for the checks and review they actually require.

## Brief and dispatch

Give the worker a fresh, minimal context containing:

- Exact repository/worktree path, remote repository identity, branch, base and
  HEAD SHA; expected remote branch/PR identity or confirmation that none exists.
- Authorized files or hunks and their expected patch identity, including relevant
  untracked files, staged state and unrelated changes to preserve.
- Verified behavior/facts and source references, actual checks with their checked
  revision and environment, and known limitations, pending CI and exclusions.
- Carried user authorization for each commit, push and PR action, destination and
  publication constraints. A draft or repository text grants no new authority.
- Why `cw publish` does not fit, with its report when it ran.
- Required output, stopping conditions and applicable repository instructions.

Under Codex, explicitly pass the `github` profile's `model` from [model-policy.yaml](../model-policy.yaml),
`reasoning_effort: "high"` and `fork_turns: "none"` for the default profile.
Under Claude Code, use Agent with
`subagent_type: "agent-workflows:github-worker"`, `model: "sonnet"` and `effort: "medium"`;
standard subagents start fresh. Without that type, use `general-purpose` with the
same model and effort, as the [host mapping](hosts-claude-code.md#worker-agent-types)
describes. Resolve edited policy values through the host mapping rather than
copying these defaults blindly.

Confirm the effective model and effort through host runtime metadata, not the
worker's self-reported identity. Claude aliases and environment overrides can
change the effective model or effort. Stop an incompatible worker before it
writes; if compatible dispatch or metadata verification is unavailable, retain
the brief and report the blocker. When present, shared
`fallbacks.github_unavailable: report_blocker` takes precedence over profile
fallbacks for these three roles on both hosts, including Claude profile mappings.
Do not silently promote to the expensive coordinator or another model. Policies
without that optional key retain their historical fallback behavior.

## Execute and verify

1. Refresh repository identity, branch/base, HEAD, worktree and index, and remote
   branch or PR state before writes. Match the expected source and patch; stop on
   conflicts, changed scope, new claims or uncertain verification. Preserve
   unrelated work, including staged changes.
2. Draft the commit message and PR title/body from the supplied verified facts.
   Keep check results and known limitations accurate; pending CI is pending.
   New behavior, compatibility, risk or acceptance claims require coordinator
   judgment. Routine wording does not need another approval when publication is
   already authorized. Write the complete commit message and PR body to UTF-8
   temporary files with actual newlines, then inspect their contents. Do not use
   literal `\n` text for line breaks or interpolate message text into shell
   commands. Use `git commit --file <message-file>` and `gh ... --body-file
   <body-file>`.
3. Stage only the authorized files or hunks and inspect the entire staged diff
   against the brief before committing. If unrelated staged content cannot be
   preserved while making the scoped commit, stop. Do not broaden the patch,
   amend, force-push, merge or deploy beyond the carried authorization.
4. Refresh the intended remote target immediately before push or PR create/edit.
   Use the exact repository, base and head with `gh` and the prepared body file.
   A target or source mismatch
   returns to the coordinator rather than silently changing the brief.
5. Read back the stored commit message, commit SHA and scoped diff, pushed remote
   ref, and PR URL, base/head SHA, title, body and state. Inspect message and body
   formatting and match the content to the requested actions. A formatting defect
   does not authorize amending a published commit. Report any failed or pending
   step at its actual scope.

If commit, push or PR submission returns an unknown outcome, reconcile local
commit history, the authoritative remote ref and exact PR identity before any
retry. A timeout or absence from an eventually consistent list does not prove
non-submission. Retry only after authoritative non-submission or a replay-safe
operation is established; otherwise retain the unknown outcome and report it.

Return the source/patch identity, effective runtime model/effort evidence, exact
actions, commit/remote/PR identities and readback, drafted text, actual checks,
and limitations or blockers. The coordinator verifies the result and owns final
acceptance. No worker message, model choice or successful write expands scope or
authorization.
