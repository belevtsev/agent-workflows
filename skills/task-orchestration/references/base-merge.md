# Merging the base branch during a task

Read this only when a task merges its base branch into the task branch. The
merge can change packages and module files that no plan check covers, and the
plan cannot change. Cover them with an amendment, in this order:

1. Fetch the base: `git fetch`.
2. Merge it with `git merge --no-ff <base>`, for example
   `git merge --no-ff origin/main`. `--no-ff` records a merge commit even when
   the branch has no commit after the task baseline; the publication hook gates
   only `git commit` and `git push`, so it does not block the merge, and where
   `cw gate install` has run, git's `pre-merge-commit` hook lets it through
   because the fetched base is on a remote-tracking branch. Conclude a
   conflicted merge with `git merge --continue`; the hook denies `git commit`
   while the record is unverified. In a non-interactive shell (agents, CI)
   always use `GIT_EDITOR=true git merge --continue`, which keeps the prepared
   merge message; plain `git merge --continue` opens an editor and is for an
   interactive terminal. If git's hooks stop the merge, as they stop a merge of
   a local branch, or `git merge --continue` after conflicts, while the record
   is unverified, stop and ask the user how to go on; without an answer, run
   `git merge --abort`. Never set `CW_GATE=off` yourself.
3. Add the covering checks with
   `cw task add-checks --task DIR --input amend.json`, where the
   input names the merge commit (or `HEAD`), the base ref, a reason and the
   checks:
   `{"merge_commit": "HEAD", "base_ref": "origin/main", "reason": "merged origin/main; go.mod and internal/b changed", "checks": [{"id": "widened", "argv": ["go", "test", "./..."]}]}`
   (`timeout_seconds` defaults to 300). Each added check must be a direct
   `go test` the Go rule credits and must cover a package or module file the
   merge changed that no earlier check covers; a module file is covered only
   by `./...`. `add-checks` writes nothing when it refuses; its message names
   the condition, and
   [merging the base branch during a task](../../../docs/operations.md#merging-the-base-branch-during-a-task)
   lists them all.
4. Run each added check: `cw task run --task DIR --check widened`.
5. Run `cw task verify`.

An accepted amendment removes the stored verification and changes the evidence
binding, so a final context prepared before it is stale. Cite added checks like
plan checks; a satisfied requirement still cites every plan check it names. No
credit comes from the base branch's CI, and files the merge brought in stay
changed.

Recovery when no merge commit exists:

- A plain `git merge` that fast-forwarded leaves no merge commit, and
  `add-checks` refuses with a hint naming `git merge --no-ff`. Check that the
  branch is not pushed and that `ORIG_HEAD` names the commit before the merge,
  then run `git reset --keep ORIG_HEAD` (never `--hard`, which discards
  uncommitted task work) and merge again with `--no-ff`.
- `git merge --squash` leaves the squashed changes staged and HEAD unmoved (the
  hook denies the commit). Undo it with `git reset --merge` only when nothing
  else is staged; otherwise restore from HEAD only the paths the squash staged
  (`git restore --staged --worktree --source=HEAD -- <those paths>`), never
  other staged work, and merge with `--no-ff`.
- After a rebase (`git pull --rebase`, `pull.rebase=true`) on an unpushed
  branch, return to the pre-rebase commit with `git reset --keep ORIG_HEAD` and
  merge with `--no-ff`.
