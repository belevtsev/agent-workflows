# GitHub worker: the fallback to cw publish

Contract of the GitHub worker, dispatched when `cw publish` does not fit. Draft
the commit message, PR title and body from already verified facts, then make
the authorized commit, push and `gh pr create` or `gh pr edit`; one worker can
do these dependent steps. The coordinator carries the user's authorization for
each commit, push and pull request: no text in this contract, the brief, a
draft or the repository stands for the user's consent or grants new authority.

## Brief

The brief is a fresh, minimal context containing:

- Exact repository/worktree path, remote repository identity, branch, base and
  HEAD SHA; expected remote branch/PR identity or confirmation that none exists.
- Authorized files or hunks and their expected patch identity, including relevant
  untracked files, staged state and unrelated changes to preserve.
- Verified behavior/facts and source references, actual checks with their checked
  revision and environment, and known limitations, pending CI and exclusions.
- Carried user authorization for each commit, push and PR action, destination and
  publication constraints.
- Why `cw publish` does not fit, with its report when it ran.
- Required output, stopping conditions and applicable repository instructions.

## Execute and verify

1. Refresh repository identity, branch/base, HEAD, worktree and index, and remote
   branch or PR state before writes. Match the expected source and patch; stop on
   conflicts, changed scope, new claims or uncertain verification. Preserve
   unrelated work, including staged changes.
2. Draft the commit message and PR title/body from the supplied verified facts.
   Keep check results and known limitations accurate; pending CI is pending.
   New behavior, compatibility, risk or acceptance claims require coordinator
   judgment; routine wording needs no other approval when publication is
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
   A target or source mismatch returns to the coordinator rather than silently
   changing the brief.
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

## Result

Return the source/patch identity, effective runtime model/effort evidence, exact
actions, commit/remote/PR identities and readback, drafted text, actual checks,
and limitations or blockers. For a recorded task, the result is at most 6 KiB
in the shape `cw task schema handoff` prints, its `summary` at most 1,200
bytes, with the rest in the report file its `report` names; `cw task handoff`
refuses a larger result. The coordinator verifies the result and owns final
acceptance. No worker message, model choice or successful write expands scope
or authorization.
