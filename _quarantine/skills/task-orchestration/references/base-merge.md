# Merging the base branch during a task

Read this only when a task merges its base branch into the task branch. The
merge can change packages and module files that no plan check covers, and the
plan cannot change. Cover them with added checks, in this order:

1. `git fetch`.
2. `git merge --no-ff <base>`, for example `git merge --no-ff origin/main`:
   `add-checks` needs a merge commit. Conclude a conflicted merge with
   `GIT_EDITOR=true git merge --continue`, never `git commit`. If git's hooks
   stop the merge or the `--continue` while the record is unverified, stop and
   ask the user how to go on; without an answer, run `git merge --abort`.
   Never set `CW_GATE=off` yourself.
3. `cw task add-checks --task <task-id> --input amend.json`; its `--help`
   prints the input. Each added check is a direct `go test` the Go rule
   credits, covering a package or module file the merge changed that no
   earlier check covers; only `./...` covers a module file. A refusal writes
   nothing and names its condition.
4. `cw task run --task <task-id> --check <id>` for each added check.
5. `cw task verify`.

A final context taken before the amendment is stale. Cite added checks like
plan checks. The base branch's CI earns no credit, and files the merge brought
in stay changed.

When `add-checks` finds no merge commit:

- After a fast-forward, or a rebase on an unpushed branch, check that the
  branch is not pushed and that `ORIG_HEAD` names the commit before it. Then
  run `git reset --keep ORIG_HEAD` (never `--hard`, which discards uncommitted
  task work) and merge with `--no-ff`.
- After `git merge --squash`, run `git reset --merge` only when nothing else
  is staged. Otherwise restore from HEAD only the paths the squash staged
  (`git restore --staged --worktree --source=HEAD -- <those paths>`), never
  other staged work. Then merge with `--no-ff`.
