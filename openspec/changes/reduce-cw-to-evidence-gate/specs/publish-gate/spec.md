# publish-gate

## Purpose

Refuse a commit or a push whose content has no recorded passing run of every
declared check, for any person or agent that uses git in the repository, and
say exactly what is missing.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` of the `check-evidence` specification: `cw.toml`
  declares `unit` (`paths = ["src/**", "unit.sh"]`) and `lint` (no `paths`);
  `unit.sh` exits 3 unless `src/app.txt` holds `ok`.
- `cw init` has run, so the hooks are installed and the gate enforces.
- A remote `origin` whose `main` is at the first commit of `demo`.
- "Checked state" means `cw run` has just ended with both checks `passed` and
  nothing has changed since.
- "Denied" means the hook exits 3, git records or sends nothing, and the
  reason is printed on standard error.

## ADDED Requirements

### Requirement: Hook installation

`cw init` SHALL install `pre-commit`, `pre-merge-commit` and `pre-push` hooks in the `hooks` directory of the repository's common git directory. Each hook SHALL run `cw hook <name>` with the arguments and input git gives it. Running `cw init` again SHALL leave the same hooks in place. `cw hook <name>` SHALL give the same decision when another hook calls it.

Source: `docs/hooks.md` (`cw gate install` today); proposal, What Changes.

#### Scenario: Hooks are installed once

- **WHEN** `cw init` runs twice in a repository that has no hooks
- **THEN** the hooks directory holds `pre-commit`, `pre-merge-commit` and `pre-push`, each is executable on Linux and macOS, and the files are identical after the first and the second run

### Requirement: Hooks of other tools

`cw init` MUST NOT change or replace a hook file it did not write, and MUST NOT write any hook when `core.hooksPath` is set. It SHALL install the hooks that are free, exit 4, and print for each other hook the line that calls `cw hook <name>`, with the statement that this line must be the last command of that hook.

Source: `docs/operations.md` exit status table (status 4, "`cw gate install` leaving a hook it did not write unchanged"); independent review of this draft, findings 3 and 4; proposal, Sources (git records the tree a later hook command staged; a relative `core.hooksPath` leaves a linked worktree without hooks).

#### Scenario: An existing pre-commit hook

- **WHEN** `pre-commit` already exists and was written by another tool, and `cw init` runs
- **THEN** the SHA-256 of `pre-commit` is unchanged, `pre-merge-commit` and `pre-push` are installed, `cw init` exits 4, and the output holds the line `cw hook pre-commit "$@"` and says that it must come last

#### Scenario: A configured hooks directory is left to its owner

- **WHEN** `git config core.hooksPath .githooks` is set and `cw init` runs
- **THEN** no file is created in `.githooks/` or in the `hooks` directory of the git directory, `cw init` exits 4, and the output holds one `cw hook` line for each of the three hooks

#### Scenario: A formatter that runs before `cw` in a shared hook

- **WHEN** another tool's `pre-commit` rewrites `src/app.txt`, stages it, and then calls `cw hook pre-commit "$@"` as its last command, and a commit of checked content is tried
- **THEN** the commit is denied, because the rewritten content has no run; after `cw run` exits 0 on the rewritten content, the same commit is recorded

### Requirement: Commit rule

The `pre-commit` hook SHALL allow a commit only when the tree that the commit would record is verified, and SHALL otherwise deny it. It SHALL judge that tree whichever way git builds it: from the index, from `git commit -a`, or from `git commit <path>`.

Source: review findings C2, H1 and H9; proposal, Sources (`git write-tree` inside `pre-commit` gives the commit's tree in all three cases).

#### Scenario: Checked content is committed

- **WHEN** `src/app.txt` is edited and still holds `ok`, `cw run` exits 0, and `git add -A` and `git commit -m change` run
- **THEN** the commit is recorded

#### Scenario: A failed check denies

- **WHEN** `src/app.txt` holds `broken`, `cw run` exits 5, and `git commit -a -m change` runs
- **THEN** the commit is denied, `HEAD` is unchanged, and the reason names `unit` as failed

#### Scenario: Unchecked content denies

- **WHEN** `src/app.txt` is edited in the checked state and `git commit -a -m change` runs without `cw run`
- **THEN** the commit is denied and the reason names `unit` and `lint` as not run on this content

#### Scenario: A new file left out of the commit denies

- **WHEN** `src/app.txt` is edited, `src/new.txt` is created, `cw run` exits 0, and `git add src/app.txt` and `git commit -m change` run
- **THEN** the commit is denied

#### Scenario: A commit of one path is judged by its own tree

- **WHEN** in the checked state `README.md` and `src/app.txt` are edited, `cw run` exits 0, and `git commit -m docs README.md` runs
- **THEN** the commit is denied, the reason names `lint` as not run on this content, and it does not name `unit`

#### Scenario: Content too large for the old fingerprint is still judged

- **WHEN** `src/app.txt` is edited and staged in the checked state, a 68 MiB untracked file `big.bin` is created, and `git commit -m change` runs
- **THEN** the commit is denied

#### Scenario: The next change needs only a new run

- **WHEN** a commit has been allowed, `src/app.txt` is edited again, `cw run` exits 0, and `git commit -a -m next` runs, with no other `cw` command in between
- **THEN** the commit is recorded

#### Scenario: A documentation change reruns only what depends on it

- **WHEN** `README.md` is edited in the checked state, `cw run` runs, and `git commit -a -m docs` runs
- **THEN** `cw run` executes `lint.sh` and not `unit.sh`, and the commit is recorded

#### Scenario: A continued cherry-pick is judged

- **WHEN** `git cherry-pick` stops on a conflict in `src/app.txt`, the conflict is resolved and staged, and `git cherry-pick --continue` runs without `cw run`
- **THEN** the commit is denied and the cherry-pick remains in progress; after `cw run` exits 0, `git cherry-pick --continue` records the commit

### Requirement: Paths that differ

When the hook denies a commit and the working tree's snapshot differs from the tree the commit would record, the reason SHALL list up to 20 of the paths that differ between the two, give the count of the rest, and say whether the snapshot itself is verified. When no snapshot can be taken, the reason SHALL say so instead.

Source: review finding H1 (`internal/gate/staged_content.go:49` walks only the staged diff).

#### Scenario: The left-out file is named

- **WHEN** `src/app.txt` is edited, `src/new.txt` is created, `cw run` exits 0, and `git add src/app.txt` and `git commit -m change` run
- **THEN** the reason lists `src/new.txt` and says that the working tree as a whole is verified

### Requirement: Merge rule

The `pre-merge-commit` hook SHALL apply the commit rule to the merge commit that `git merge` is about to record. After a denial the merge SHALL remain in progress, so that `cw run` followed by `git commit` concludes it.

Source: proposal, Sources (a denying `pre-merge-commit` leaves `MERGE_HEAD` in place, and `git commit` then runs `pre-commit`).

#### Scenario: A merge is checked before it is recorded

- **WHEN** branch `feature` adds `docs/a.md`, `main` has a later commit of its own, and `git merge feature` runs on `main`
- **THEN** the merge is denied, the reason names `lint` as not run on this content, and `git status` reports a merge in progress

#### Scenario: The merge is concluded after a run

- **WHEN** after that denial `cw run` exits 0 and `git commit --no-edit` runs
- **THEN** `HEAD` is a commit with two parents

### Requirement: Push rule

For each ref that a push updates, the `pre-push` hook SHALL allow the update when it deletes the ref or adds nothing, and otherwise only when the tree of the new tip commit is verified. It SHALL judge the tip commit only, not the commits below it. One denied update SHALL deny the whole push.

Source: review finding M26 (`internal/gate/gate.go` judges a push by the working tree); proposal, Sources (`pre-commit` does not run for a clean cherry-pick or revert, a rebase or a fast-forward merge) and Open Question 8; independent review of this draft, finding 1.

#### Scenario: A commit the gate allowed is pushed

- **WHEN** a commit has been allowed on `main` and `git push origin main` runs
- **THEN** the push is sent

#### Scenario: A commit made around the hook is caught

- **WHEN** `src/app.txt` holds `broken`, `git commit -a --no-verify -m skip` records it, and `git push origin main` runs
- **THEN** the push is denied, `origin/main` is unchanged, and the reason names `unit`

#### Scenario: A cherry-picked commit needs a run

- **WHEN** `git cherry-pick` adds a commit to `main` whose tree has no run of `lint`, and `git push origin main` runs
- **THEN** the push is denied; after `cw run` exits 0 in the clean working tree, the same push is sent

#### Scenario: The branch that is pushed is the one judged

- **WHEN** the working tree on `main` is in the checked state, the tip of branch `feature` has a tree on which `unit` failed, and `git push origin feature` runs
- **THEN** the push is denied, and the reason names `refs/heads/feature` and says that `cw run` must run in a checkout of that commit

#### Scenario: Only the tip is judged

- **WHEN** `git rebase main` rewrites three commits of `feature`, `cw run` exits 0 in the clean working tree, and `git push origin feature` runs
- **THEN** the push is sent, although the two commits below the tip have no run

#### Scenario: An unchecked branch of the remote does not reach `main` unnoticed

- **WHEN** `origin/wip` holds a commit that has no run in this repository, `git merge --ff-only origin/wip` moves `main` to it, and `git push origin main` runs
- **THEN** the push is denied; after `cw run` exits 0, the same push is sent

#### Scenario: One bad ref denies the whole push

- **WHEN** `main` has a verified tip, `feature` does not, and `git push origin main feature` runs
- **THEN** the push is denied and neither ref changes on `origin`

#### Scenario: A dry run is judged like a push

- **WHEN** the tip of `main` is not verified and `git push --dry-run origin main` runs
- **THEN** the push is denied

### Requirement: Updates that add nothing

An update of a branch SHALL count as adding nothing when the commit that branch points at on the remote already contains the new tip. An update of any other ref SHALL count as adding nothing when a remote-tracking branch of the same remote contains the commit it points at. Such an update, and the deletion of a ref, SHALL be allowed whatever the state of the working tree and of the records.

Source: review finding M26 (`git push origin --delete old-branch` was denied); proposal, Sources (`pre-push` input for a delete and for a tag on a commit the remote has); independent review of this draft, finding 5.

#### Scenario: A branch is deleted on the remote

- **WHEN** `origin` has a branch `old-branch`, the working tree is not verified, and `git push origin --delete old-branch` runs
- **THEN** the push is sent

#### Scenario: A tag on a commit the remote already has

- **WHEN** tag `v1` points at a commit that `origin/main` contains, no record exists for its tree, and `git push origin v1` runs
- **THEN** the push is sent

#### Scenario: A branch is moved back

- **WHEN** `origin/main` is two commits ahead of the first commit, no record exists for the first commit's tree, and `git push --force origin HEAD~2:main` runs on `main`
- **THEN** the push is sent

#### Scenario: A new branch at a commit another branch has

- **WHEN** no record exists for the tree of `origin/main`, and `git push origin origin/main:refs/heads/release` runs
- **THEN** the push is denied, because `release` does not exist on the remote and its new tip is not verified

### Requirement: Hooks decide from records

A hook SHALL decide from recorded runs alone. It SHALL NOT execute a check, SHALL NOT wait for a check that is running, and SHALL NOT change the index, the working tree or any ref.

Source: proposal, Success measure 2; review finding M4 (0.41 s per load inside the commit hook at 200 runs).

#### Scenario: A denied commit starts no check

- **WHEN** `src/app.txt` is edited in the checked state and `git commit -a -m change` runs
- **THEN** the commit is denied, and neither `unit.sh` nor `lint.sh` is executed

#### Scenario: A running check does not hold up a commit

- **WHEN** `cw run` is executing a check that sleeps for 60 seconds, and `git commit -a -m change` runs in another terminal
- **THEN** the hook decides within 2 seconds

### Requirement: Fail closed

When an enforcing hook cannot establish that the content is verified, it SHALL deny with exit status 3 and name the cause. This covers a tree it cannot compute, a missing or invalid declaration, a store it cannot read, and an internal error. An installed hook SHALL deny, with an instruction to install `cw`, when `cw` is not on `PATH`.

Source: review finding C2 (`internal/taskflow/find.go:254-256` skips a record whose digest cannot be computed); `SECURITY.md` (the gate fails open by design today).

#### Scenario: `cw` is missing

- **WHEN** `cw` is removed from `PATH` and `git commit -a -m change` runs
- **THEN** git records nothing, and the output says that `cw` was not found, says where the install instruction is, and names no Go command

#### Scenario: The declaration is invalid

- **WHEN** line 9 of the staged `cw.toml` is `tiemout = "5m"` and `git commit -m change` runs
- **THEN** the commit is denied and the reason holds `cw.toml:9`

#### Scenario: The declaration is gone

- **WHEN** `git rm cw.toml` and `git commit -m remove` run
- **THEN** the commit is denied and the reason says that the tree has no declaration

#### Scenario: The store cannot be read

- **WHEN** the `cw/` directory in the git directory is made unreadable and `git commit -a -m change` runs
- **THEN** the commit is denied and the reason names that directory

### Requirement: Denial message

A denial SHALL name each check that is not satisfied with its state, which is not run on this content, failed or timed out, and with the log of the deciding run when there is one. It SHALL end with the command that produces the missing evidence, and SHALL say where to run it when the denied content is not the working tree's. It MUST NOT name `--no-verify`, a setting or any other way to skip the gate.

Source: review finding L1 (`internal/gate/gate.go:368` tells the agent how to turn the gate off); independent review of this draft, finding 9.

#### Scenario: What a denial says

- **WHEN** `unit` failed and `lint` was never run on the content of a commit, and the commit is denied
- **THEN** the reason holds `unit`, `failed`, the path of the log of that run, `lint`, `not run`, and the command `cw run`

#### Scenario: What a denial does not say

- **WHEN** any commit or push is denied
- **THEN** the reason holds neither `--no-verify` nor `observe` nor `uninstall`

### Requirement: Declaration change notice

When the tree a commit would record lacks a check that the tree of `HEAD` declares, or declares one with a different `run`, `paths` or `timeout`, the hook SHALL print a notice that lists each such check and what changed, whether it allows or denies the commit.

Source: proposal, Open Question 4.

#### Scenario: A check is removed

- **WHEN** the staged `cw.toml` no longer declares `unit`, `cw run` exits 0, and `git commit -a -m change` runs
- **THEN** the commit is recorded and standard error holds a notice that `unit` is no longer declared

#### Scenario: A check is added

- **WHEN** the staged `cw.toml` adds a check `build` and changes nothing else
- **THEN** the hook prints no such notice

### Requirement: Linked worktrees

The hooks that `cw init` installs SHALL apply in every linked worktree of the repository. A run recorded in one worktree SHALL count in another for the same checked content. Work in one worktree that is unfinished, unchecked or failing SHALL NOT change a decision about different content in another.

Source: review finding H9 (`docs/hooks.md:212-214`; a task in the main checkout blocks commits in every linked worktree).

#### Scenario: A failing main checkout does not block a worktree

- **WHEN** `src/app.txt` holds `broken` in the main checkout and `cw run` there exits 5, and in the linked worktree `../wt` `README.md` is edited, `cw run` exits 0, and `git commit -a -m docs` runs
- **THEN** the commit in `../wt` is recorded

#### Scenario: A worktree is gated too

- **WHEN** `src/app.txt` is edited in `../wt` and `git commit -a -m change` runs there without `cw run`
- **THEN** the commit is denied

### Requirement: Observe mode

`cw init --observe` SHALL put the repository's gate in observe mode, and `cw init` without that option SHALL return it to enforcing. In observe mode each hook SHALL record the decision it would have made, print it in one line, and allow the operation whatever that decision is, also when a cause prevents a decision.

Source: review finding H7 (no baseline exists for the workflow's benefit); proposal, Success measure 5; independent review of this draft, finding 12.

#### Scenario: A commit that would be denied goes through and is counted

- **WHEN** the gate is in observe mode, `src/app.txt` holds `broken`, `cw run` exits 5, and `git commit -a -m change` runs
- **THEN** the commit is recorded, standard error holds one line that says it would have been denied because `unit` failed, and `cw stats` counts one observed denial

#### Scenario: A cause that prevents a decision is observed too

- **WHEN** the gate is in observe mode, line 9 of the staged `cw.toml` is `tiemout = "5m"`, and `git commit -m change` runs
- **THEN** the commit is recorded and standard error holds one line that says it would have been denied because the declaration is invalid

#### Scenario: Enforcing again

- **WHEN** `cw init` runs after `cw init --observe`, and the same kind of commit is tried
- **THEN** the commit is denied

### Requirement: Removal

`cw uninstall` SHALL remove the hooks that `cw` wrote and the `cw/` store, and SHALL leave `cw.toml` and every hook file that `cw` did not write unchanged. When such a file still calls `cw hook`, `cw uninstall` SHALL name the file, print the line to remove from it, and exit 4.

Source: review finding L3 (`internal/codexinstall/codexinstall.go:142-160` removes links it does not own); independent review of this draft, finding 14.

#### Scenario: Only `cw`'s own files go

- **WHEN** `pre-commit` and `pre-push` were written by `cw` and `cw uninstall` runs
- **THEN** it exits 0, both hooks and the `cw/` directory are gone, and `cw.toml` has the same SHA-256 as before

#### Scenario: Another tool's hook still calls `cw`

- **WHEN** `pre-commit` was written by another tool and calls `cw hook pre-commit`, `pre-push` was written by `cw`, and `cw uninstall` runs
- **THEN** `pre-push` and the `cw/` directory are gone, `pre-commit` has the same SHA-256 as before, and `cw uninstall` exits 4 and prints the path of `pre-commit` and the line to remove
