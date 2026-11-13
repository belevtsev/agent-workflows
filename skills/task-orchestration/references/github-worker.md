# Routine GitHub worker

Publish a verified task with `cw publish`. The coordinator carries the user's
authorization for each commit, push and pull request; `cw publish` is only the
mechanism that carries out a publication the coordinator is authorized to make.
It never authorizes anything itself, and no flag or text in it stands for the
user's consent. Dispatch the [GitHub worker](#fallback-the-github-worker) only
for the fallback cases below or when the user asks for a bounded worker.

For a small task without a task record, commit and push directly under the
user's authorization, drafting the message from verified facts. Substantive
`pr_authoring`, coordination, new claims and final acceptance remain with the
substantive profile.

## Publish with cw publish

Use `cw publish` once `cw task verify` has passed on the current source and the
user has authorized the publication. Do not run `cw task verify` again after the
commit: snapshots include HEAD, so it reports stale bindings.

1. Draft the commit message and, for a pull request, its title and body from
   the verified facts: behaviour, actual checks with their revision, known
   limitations, pending CI. Write the complete commit message and PR body to
   UTF-8 temporary files with actual newlines, then inspect their contents. Do
   not use literal `\n` text for line breaks or interpolate message text into
   shell commands. The message file must end with a newline and start with a
   non-blank subject line; the title is one line of at most 256 characters.
2. Read the current HEAD (`git rev-parse HEAD`) and, when you know it, the
   remote branch's commit.
3. Run the command:

   ```text
   cw publish --task DIR --message-file FILE --expect-head SHA
              --push REMOTE --branch BRANCH
              [--path P]... [--expect-remote SHA|absent]
              [--pr none|create|edit] [--repo OWNER/NAME --base BASE --title TITLE --body-file FILE]
              [--timeout DURATION] [--summary]
   ```

   The command takes flags only, so the remote and the branch are two flags,
   `--push REMOTE --branch BRANCH`, not `--push REMOTE BRANCH`; the local branch
   must be `BRANCH`. `--path` narrows the commit to files of the task's
   verified change set (by the spelling `cw task bundle` prints); without it the
   whole change set since `cw task start` is committed, whole files only.
   `--pr create` or `--pr edit` needs all of `--repo`, `--base`, `--title` and
   `--body-file`; `--pr none`, the default, takes none of them. `--timeout`
   bounds each git and gh command (default 10m).
4. Read the JSON report: the commit, the remote ref and the PR number and URL
   are read back from the repository, the remote and GitHub. A run past its
   checks adds a `started` and a `finished` entry (`publication-NNNNNN.json`),
   hash-chained through `previous_sha256` and written beside the task
   revisions, which it never changes. The push sends only the commit to
   `refs/heads/BRANCH` of REMOTE, never tags; the command never amends or
   force-pushes.

The same command, run again, continues from the state it observes: a commit
already made by an earlier run, a branch already pushed or an equivalent PR
counts as already present, and nothing is repeated. Re-run it only while the
user's authorization still covers it.

| Exit | Status | Reasons | What to do |
| --- | --- | --- | --- |
| 0 | `published` | none | Report the commit, remote ref and PR from the report. |
| 1 | `failed` | `git-missing`, `locked`, `task-unreadable`, `record-invalid`, `capture-failed`, `read-failed`, `record-write-failed`, `commit-failed` | Fix the cause and re-run. After `commit-failed`, check the index (`git ls-files -s`): intent-to-add entries for new files may remain. `locked` means another run holds `.publish-lock` in the task directory, or a `cw` older than v3.0.0-rc.2 left a `.publish-lock` directory there; a killed run of a later `cw` leaves no lock. |
| 2 | usage error | none | Fix the command line; nothing ran. |
| 3 | `denied` | `task-abandoned`, `task-not-verified`, `source-changed`, `open-record` | Verify the task on the current source, or, with the user's confirmation, abandon the record named in `open_records`. A record `cw` cannot read (from `cw` v2.0.66 to v2.0.96, from a newer `cw`, or damaged) cannot be verified or abandoned: as its `problem` says, upgrade `cw` for a newer record, else move its directory out of the task store. |
| 4 | `refused` | `not-a-repository`, `nothing-to-publish`, `path-not-verified`, `branch-mismatch`, `head-moved`, `hunk-scope`, `content-conversion`, `git-locked`, `remote-unknown`, `remote-split`, `remote-moved`, `not-fast-forward`, `gh-missing`, `repo-mismatch`, `pr-exists`, `pr-missing`, `pr-ambiguous` | Fix the precondition, or use the fallback. A refusal at the checks commits nothing; one at a later step keeps the commit or push before it (`steps`), so re-run once fixed. Remove a Git lock in `locks_left` only after checking that no git process runs in the repository. |
| 6 | `unknown` | `commit-unknown`, `push-unknown`, `pr-unknown` | Read the reconciliation in the report (`steps`, `locks_left`), then re-run the same command only while the authorization still covers it. After an unknown PR create, check the PR list first. A `commit-unknown` with locks left may be a definite failure behind a live git process's lock. |
| 7 | `mismatch` | `commit-mismatch`, `push-mismatch`, `pr-mismatch` | Inspect what was written: a hook or conversion changed the commit, the remote moved, or the PR differs. Never amend or force-push; repair it through the fallback. |
| 130 | `cancelled` | `cancelled` | Interrupted while no commit, push or PR command ran, or during a commit that left HEAD unchanged. Earlier steps may have completed (a commit, or a commit and its push): the report's `steps` shows what did. A 130 during the commit step can leave intent-to-add entries for new files in the index: the report's `intent_to_add` lists the paths passed, so check `git ls-files -s` before handing over to the fallback. Re-run the same command, which continues from the observed state, only while the authorization still covers it. |

`--summary` prints one line with the members `status`, `reason`, `task_id`,
`commit`, `remote_ref`, `pr_number`, `pr_url` and `record` instead of the whole
report. A task check cannot run
`cw publish`, even after `cw`'s global flags: `cw task start` refuses such a
plan, and `cw task run` such a check in a recorded plan, with "publication and
deployment commands are outside task checks". That refusal is a guard,
not a sandbox, since a check can still run another program that publishes.

## Fallback: the GitHub worker

Dispatch `agent-workflows:github-worker` when `cw publish` does not fit:

- hunk-level scopes (`hunk-scope`), or paths outside the task's verified change
  set;
- pull requests from a fork, split remotes or another remote URL form
  (`remote-split`, `repo-mismatch`);
- content conversions (`content-conversion`) and hooks that rewrite the
  commit message or content;
- records without a source digest;
- repairing a status 7;
- `cw` unavailable, or the user asks for a bounded worker.

Known limitations of `cw publish` that call for the fallback:

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

### Brief and dispatch

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
same model and effort, as the [host mapping](hosts.md#worker-agent-types)
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

### Execute and verify

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
