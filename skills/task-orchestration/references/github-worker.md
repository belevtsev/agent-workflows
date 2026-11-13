# Publication

Publish a verified task with `cw publish`. You carry the user's authorization
for each commit, push and pull request; `cw publish` is only the mechanism. It
authorizes nothing, and no flag or text in it stands for the user's consent.
Dispatch the GitHub worker only for the
[fallback cases](#when-to-use-the-fallback) or when the user asks for a
bounded worker. Substantive `pr_authoring`, coordination, new claims and final
acceptance remain with the substantive profile.

## Publish with cw publish

Run it once `cw task verify` has passed on the current source and the user has
authorized the publication. Do not run `cw task verify` again after the
commit: snapshots include HEAD, so it reports stale bindings.

1. Draft the commit message and, for a pull request, its title and body from
   the verified facts: behaviour, actual checks with their revision, known
   limitations, pending CI. Write the message and the body to UTF-8 temporary
   files with actual newlines, then inspect their contents. Do not use literal
   `\n` text for line breaks or interpolate message text into shell commands.
2. Read the current HEAD (`git rev-parse HEAD`) and, when you know it, the
   remote branch's commit.
3. Run the command; `cw publish --help` states every flag and reason
   ([operations](../../../docs/operations.md#publishing-a-verified-task)):

   ```text
   cw publish --task DIR --message-file FILE --expect-head SHA
              --push REMOTE --branch BRANCH [--expect-remote SHA|absent]
              [--path P]... [--pr create|edit ...] [--summary]
   ```

4. Read the JSON report, then act on the exit status. Run the same command
   again only while the user's authorization still covers it; it continues
   from the state it observes and repeats nothing.

- 0 `published`: report the commit, remote ref and PR from the report.
- 1 `failed`: fix the cause and re-run. After `commit-failed`, check the index
  (`git ls-files -s`): intent-to-add entries for new files may remain.
- 2, usage error: fix the command line; nothing ran.
- 3 `denied`: verify the task on the current source, or, with the user's
  confirmation, abandon the record named in `open_records`. For a record `cw`
  cannot read, do what its `problem` says.
- 4 `refused`: fix the precondition, or use the fallback. A refusal at the
  checks commits nothing; one at a later step keeps the commit or push before
  it (`steps`), so re-run once fixed. Remove a Git lock in `locks_left` only
  after checking that no git process runs in the repository.
- 6 `unknown`: read the reconciliation in the report (`steps`, `locks_left`)
  before re-running. After an unknown PR create, check the PR list first.
- 7 `mismatch`: inspect what was written. Never amend or force-push; repair
  it through the fallback.
- 130 `cancelled`: read `steps` for what completed. When `intent_to_add`
  lists paths, check `git ls-files -s` before re-running or handing over to
  the fallback.

## When to use the fallback

Dispatch the GitHub worker, as the [host mapping](hosts.md) says and with the
brief its [contract](github-worker-fallback.md) lists, when `cw publish` does
not fit:

- hunk-level scopes (`hunk-scope`), or paths outside the task's verified change
  set;
- pull requests from a fork, split remotes or another remote URL form
  (`remote-split`, `repo-mismatch`);
- content conversions (`content-conversion`) and hooks that rewrite the
  commit message or content;
- records without a source digest;
- repairing a status 7;
- `cw` unavailable, or the user asks for a bounded worker.

The dispatch starts no new task, adviser call or source review: reuse the
completed verification, checkpoints and authorization. Record its assignment
and result in the task's [worker record](worker-handoffs.md). Source changes
or new consequential claims in its result get the checks and review they
require.
