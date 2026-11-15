# publish-gate

## Purpose

Refuse a commit or a push whose content has no recorded passing run of every
declared check, for any person or agent that uses git in the repository, and
say exactly what is missing.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` of the `check-evidence` specification: `gatecub.toml`
  declares `unit` (`paths = ["src/**", "unit.sh"]`) and `lint` (no `paths`);
  `unit.sh` exits 3 unless `src/app.txt` holds `ok`.
- `gatecub init` has run, so the hooks are installed and the gate enforces.
- A remote `origin` whose `main` is at the first commit of `demo`.
- "Checked state" means `gatecub run` has just ended with both checks `passed` and
  nothing has changed since.
- "Denied" means the hook exits 3, git records or sends nothing, and the
  reason is printed on standard error.

## ADDED Requirements

### Requirement: Hook installation

`gatecub init` SHALL install `pre-commit`, `pre-merge-commit` and `pre-push` hooks in the `hooks` directory of the repository's common git directory. Each hook SHALL run `gatecub hook <name>` with the arguments and input git gives it. Running `gatecub init` again SHALL leave the same hooks in place. `gatecub hook <name>` SHALL give the same decision when another hook calls it.

Source: `docs/hooks.md` (`gatecub gate install` today); proposal, What Changes.

#### Scenario: Hooks are installed once

- **WHEN** `gatecub init` runs twice in a repository that has no hooks
- **THEN** the hooks directory holds `pre-commit`, `pre-merge-commit` and `pre-push`, each is executable on Linux and macOS, and the files are identical after the first and the second run

### Requirement: Hooks of other tools

`gatecub init` MUST NOT change or replace a hook file it did not write, and MUST NOT write any hook when `core.hooksPath` is set. It SHALL install the hooks that are free. For each other hook that does not yet call `gatecub hook <name>` it SHALL print the line that does, with the statement that this line must be the last command of that hook, and exit 4.

Source: `docs/operations.md` exit status table (status 4, "`gatecub gate install` leaving a hook it did not write unchanged"); independent review of this draft, findings 3 and 4; proposal, Sources (git records the tree a later hook command staged; a relative `core.hooksPath` leaves a linked worktree without hooks).

#### Scenario: An existing pre-commit hook

- **WHEN** `pre-commit` already exists and was written by another tool, and `gatecub init` runs
- **THEN** the SHA-256 of `pre-commit` is unchanged, `pre-merge-commit` and `pre-push` are installed, `gatecub init` exits 4, and the output holds the line `gatecub hook pre-commit "$@"` and says that it must come last

#### Scenario: A configured hooks directory is left to its owner

- **WHEN** `git config core.hooksPath .githooks` is set and `gatecub init` runs
- **THEN** no file is created in `.githooks/` or in the `hooks` directory of the git directory, `gatecub init` exits 4, and the output holds one `gatecub hook` line for each of the three hooks

#### Scenario: A shared hook that already calls `gatecub`

- **WHEN** `pre-commit` was written by another tool and its last command is `gatecub hook pre-commit "$@"`, and `gatecub init` runs
- **THEN** it exits 0, the SHA-256 of `pre-commit` is unchanged, and `gatecub status --json` holds `hooks` as `installed`

#### Scenario: A formatter that runs before `gatecub` in a shared hook

- **WHEN** another tool's `pre-commit` rewrites `src/app.txt`, stages it, and then calls `gatecub hook pre-commit "$@"` as its last command, and a commit of checked content is tried
- **THEN** the commit is denied, because the rewritten content has no run; after `gatecub run` exits 0 on the rewritten content, the same commit is recorded

### Requirement: Commit rule

The `pre-commit` hook SHALL allow a commit only when the tree that the commit would record is verified, and SHALL otherwise deny it. It SHALL judge that tree whichever way git builds it: from the index, from `git commit -a`, or from `git commit <path>`.

Source: review findings C2, H1 and H9; proposal, Sources (`git write-tree` inside `pre-commit` gives the commit's tree in all three cases).

#### Scenario: Checked content is committed

- **WHEN** `src/app.txt` is edited and still holds `ok`, `gatecub run` exits 0, and `git add -A` and `git commit -m change` run
- **THEN** the commit is recorded

#### Scenario: A failed check denies

- **WHEN** `src/app.txt` holds `broken`, `gatecub run` exits 5, and `git commit -a -m change` runs
- **THEN** the commit is denied, `HEAD` is unchanged, and the reason names `unit` as failed

#### Scenario: Unchecked content denies

- **WHEN** `src/app.txt` is edited in the checked state and `git commit -a -m change` runs without `gatecub run`
- **THEN** the commit is denied and the reason names `unit` and `lint` as not run on this content

#### Scenario: A new file left out of the commit denies

- **WHEN** `src/app.txt` is edited, `src/new.txt` is created, `gatecub run` exits 0, and `git add src/app.txt` and `git commit -m change` run
- **THEN** the commit is denied

#### Scenario: A commit of one path is judged by its own tree

- **WHEN** in the checked state `README.md` and `src/app.txt` are edited, `gatecub run` exits 0, and `git commit -m docs README.md` runs
- **THEN** the commit is denied, the reason names `lint` as not run on this content, and it does not name `unit`

#### Scenario: Content too large for the old fingerprint is still judged

- **WHEN** `src/app.txt` is edited and staged in the checked state, a 68 MiB untracked file `big.bin` is created, and `git commit -m change` runs
- **THEN** the commit is denied

#### Scenario: The next change needs only a new run

- **WHEN** a commit has been allowed, `src/app.txt` is edited again, `gatecub run` exits 0, and `git commit -a -m next` runs, with no other `gatecub` command in between
- **THEN** the commit is recorded

#### Scenario: A documentation change reruns only what depends on it

- **WHEN** `README.md` is edited in the checked state, `gatecub run` runs, and `git commit -a -m docs` runs
- **THEN** `gatecub run` executes `lint.sh` and not `unit.sh`, and the commit is recorded

#### Scenario: A continued cherry-pick is judged

- **WHEN** `git cherry-pick` stops on a conflict in `src/app.txt`, the conflict is resolved and staged, and `git cherry-pick --continue` runs without `gatecub run`
- **THEN** the commit is denied and the cherry-pick remains in progress; after `gatecub run` exits 0, `git cherry-pick --continue` records the commit

### Requirement: Paths that differ

When the hook denies a commit and the working tree's snapshot differs from the tree the commit would record, the reason SHALL list up to 20 of the paths that differ between the two, give the count of the rest, and say whether the snapshot itself is verified, how to stage the rest and how to set it aside. A path that holds a control character SHALL be printed quoted. When no snapshot can be taken, the reason SHALL say so instead.

Source: review finding H1 (`internal/gate/staged_content.go:49` walks only the staged diff); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding G10 and U4.

#### Scenario: The left-out file is named

- **WHEN** `src/app.txt` is edited, `src/new.txt` is created, `gatecub run` exits 0, and `git add src/app.txt` and `git commit -m change` run
- **THEN** the reason lists `src/new.txt` and says that the working tree as a whole is verified


#### Scenario: The denial says how to set the rest aside

- **WHEN** in the checked state `README.md` is edited and staged, `notes.txt` is created and not staged, and `git commit -m docs` runs
- **THEN** the commit is denied, and the reason names `notes.txt`, `git add -A` and `git stash push --keep-index --include-untracked`

#### Scenario: A path with a line break is quoted

- **WHEN** in the checked state `README.md` is edited and staged, a file whose name holds a line break followed by `gatecub: allowed` is created and not staged, and `git commit -m docs` runs
- **THEN** the commit is denied, the reason lists that name quoted, as `\n` within double quotes, and no line of the reason begins with `gatecub: allowed`
### Requirement: Merge rule

The `pre-merge-commit` hook SHALL apply the commit rule to the merge commit that `git merge` is about to record. After a denial the merge SHALL remain in progress, so that `gatecub run` followed by `git commit` concludes it.

Source: proposal, Sources (a denying `pre-merge-commit` leaves `MERGE_HEAD` in place, and `git commit` then runs `pre-commit`).

#### Scenario: A merge is checked before it is recorded

- **WHEN** branch `feature` adds `docs/a.md`, `main` has a later commit of its own, and `git merge feature` runs on `main`
- **THEN** the merge is denied, the reason names `lint` as not run on this content, and `git status` reports a merge in progress

#### Scenario: The merge is concluded after a run

- **WHEN** after that denial `gatecub run` exits 0 and `git commit --no-edit` runs
- **THEN** `HEAD` is a commit with two parents

### Requirement: Push rule

For each ref that a push updates, the `pre-push` hook SHALL allow the update when it deletes the ref or adds nothing, and otherwise only when the tree of the new tip commit is verified. It SHALL judge the tip commit only, not the commits below it. One denied update SHALL deny the whole push. The denial SHALL send to a checkout of the commit only when the working tree holds other checked content for a check that is not satisfied.

Source: review finding M26 (`internal/gate/gate.go` judges a push by the working tree); proposal, Sources (`pre-commit` does not run for a clean cherry-pick or revert, a rebase or a fast-forward merge) and Open Question 8; independent review of this draft, finding 1; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding U1, U5 and G5.

#### Scenario: A commit the gate allowed is pushed

- **WHEN** a commit has been allowed on `main` and `git push origin main` runs
- **THEN** the push is sent

#### Scenario: A commit made around the hook is caught

- **WHEN** `src/app.txt` holds `broken`, `git commit -a --no-verify -m skip` records it, and `git push origin main` runs
- **THEN** the push is denied, `origin/main` is unchanged, and the reason names `unit`

#### Scenario: A cherry-picked commit needs a run

- **WHEN** `git cherry-pick` adds a commit to `main` whose tree has no run of `lint`, and `git push origin main` runs
- **THEN** the push is denied; after `gatecub run` exits 0 in the clean working tree, the same push is sent

#### Scenario: The branch that is pushed is the one judged

- **WHEN** the working tree on `main` is in the checked state, the tip of branch `feature` has a tree on which `unit` failed, and `git push origin feature` runs
- **THEN** the push is denied, and the reason names `refs/heads/feature` and says that `gatecub run` must run in a checkout of that commit

#### Scenario: Only the tip is judged

- **WHEN** `git rebase main` rewrites three commits of `feature`, `gatecub run` exits 0 in the clean working tree, and `git push origin feature` runs
- **THEN** the push is sent, although the two commits below the tip have no run

#### Scenario: An unchecked branch of the remote does not reach `main` unnoticed

- **WHEN** `origin/wip` holds a commit that has no run in this repository, `git merge --ff-only origin/wip` moves `main` to it, and `git push origin main` runs
- **THEN** the push is denied; after `gatecub run` exits 0, the same push is sent

#### Scenario: One bad ref denies the whole push

- **WHEN** `main` has a verified tip, `feature` does not, and `git push origin main feature` runs
- **THEN** the push is denied and neither ref changes on `origin`

#### Scenario: A dry run is judged like a push

- **WHEN** the tip of `main` is not verified and `git push --dry-run origin main` runs
- **THEN** the push is denied

#### Scenario: An annotated tag is judged by the commit it names

- **WHEN** annotated tag `v2` names a commit that `origin` does not have and whose tree is not verified, and `git push origin v2` runs
- **THEN** the push is denied and the reason names `refs/tags/v2`


#### Scenario: A change outside the checked paths needs no other checkout

- **WHEN** every check of `gatecub.toml` has `paths`, the tip of `main` has no run, the working tree differs from it only by the untracked file `notes.txt`, and `git push origin main` runs
- **THEN** the push is denied, and the reason ends with the instruction to run `gatecub run` and holds no `git worktree add`; after `gatecub run` exits 0, the same push is sent

#### Scenario: A branch without project code needs a declaration

- **WHEN** an orphan branch `site` holds only `index.html`, and `git push origin site` runs
- **THEN** the push is denied and the reason names `docs/operations.md#a-tree-without-gatecubtoml`; after a commit adds a `gatecub.toml` that holds only `version = 1`, the same push is sent without any run
### Requirement: Updates that add nothing

An update of a branch SHALL count as adding nothing when the commit that branch points at on the remote already contains the new tip. An update of any other ref SHALL count as adding nothing when a remote-tracking branch of the same remote, or a branch that the same push updates, contains the commit it points at. An update under `refs/notes/` SHALL count as adding nothing. Such an update, and the deletion of a ref, SHALL be allowed whatever the state of the working tree and of the records.

Source: review finding M26 (`git push origin --delete old-branch` was denied); proposal, Sources (`pre-push` input for a delete and for a tag on a commit the remote has); independent review of this draft, finding 5; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding G5.

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


#### Scenario: Notes are not judged

- **WHEN** `git notes add` has made `refs/notes/commits`, the working tree is not verified, no run is recorded, and `git push origin refs/notes/commits` runs
- **THEN** the push is sent

#### Scenario: A tag below a branch of the same push

- **WHEN** `origin` is empty, tag `v0` names an older commit whose tree has no `gatecub.toml`, the verified tip of `main` contains that commit, and `git push origin v0` and then `git push origin main v0` run
- **THEN** the first push is denied, and the second is sent with both refs
### Requirement: Hooks decide from records

A hook SHALL decide from recorded runs alone. It SHALL NOT execute a check, SHALL NOT wait for a check that is running, and SHALL NOT change the index, the working tree or any ref.

Source: proposal, Success measure 2; review finding M4 (0.41 s per load inside the commit hook at 200 runs).

#### Scenario: A denied commit starts no check

- **WHEN** `src/app.txt` is edited in the checked state and `git commit -a -m change` runs
- **THEN** the commit is denied, and neither `unit.sh` nor `lint.sh` is executed

#### Scenario: A running check does not hold up a commit

- **WHEN** `gatecub run` is executing a check that sleeps for 60 seconds, and `git commit -a -m change` runs in another terminal
- **THEN** the hook decides within 2 seconds

### Requirement: Fail closed

When an enforcing hook cannot establish that the content is verified, it SHALL deny with exit status 3 and name the cause. This covers a tree it cannot compute, a missing or invalid declaration, a store it cannot read, and an internal error. An installed hook SHALL look for `gatecub` only in the entries of `PATH` that are absolute paths, and SHALL deny, with an instruction to install `gatecub`, when none of them holds it. A denial of a tree without `gatecub.toml` SHALL name the documents about such a tree.

Source: review finding C2 (`internal/taskflow/find.go:254-256` skips a record whose digest cannot be computed); `SECURITY.md` (the gate fails open by design today); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding S1 and U1.

#### Scenario: `gatecub` is missing

- **WHEN** `gatecub` is removed from `PATH` and `git commit -a -m change` runs
- **THEN** git records nothing, and the output says that `gatecub` was not found, says where the install instruction is, and names no Go command

#### Scenario: The declaration is invalid

- **WHEN** line 9 of the staged `gatecub.toml` is `tiemout = "5m"` and `git commit -m change` runs
- **THEN** the commit is denied and the reason holds `gatecub.toml:9`

#### Scenario: The declaration is gone

- **WHEN** `git rm gatecub.toml` and `git commit -m remove` run
- **THEN** the commit is denied and the reason says that the tree has no declaration

#### Scenario: The store cannot be read

- **WHEN** the `gatecub/` directory in the git directory is made unreadable and `git commit -a -m change` runs
- **THEN** the commit is denied and the reason names that directory


#### Scenario: A tree without `gatecub.toml` names the documents

- **WHEN** `git rm gatecub.toml` and `git commit -m remove` run
- **THEN** the commit is denied, the reason names `docs/operations.md#a-tree-without-gatecubtoml`, and it holds neither `gatecub init` nor `gatecub uninstall`

#### Scenario: An empty or relative entry of `PATH`

- **WHEN** the first entry of `PATH` is empty or `.`, no other entry holds `gatecub`, the repository root holds an executable file `gatecub`, and `git commit -a -m change` runs
- **THEN** git records nothing, the output says that `gatecub` was not found in an entry of `PATH` that is an absolute path, and the file `gatecub` of the repository is not run
### Requirement: Denial message

A denial SHALL name each check that is not satisfied with its state, which is not run on this content, failed or timed out, and with the log of the deciding run when there is one. It SHALL say to fix the log first when a check failed or timed out, SHALL end with the command that produces the missing evidence, and SHALL say where to run it when the working tree holds other checked content. It MUST NOT name `--no-verify`, a setting or any other way to skip the gate.

Source: review finding L1 (`internal/gate/gate.go:368` tells the agent how to turn the gate off); independent review of this draft, finding 9; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding U2, U3 and U5.

#### Scenario: What a denial says

- **WHEN** `unit` failed and `lint` was never run on the content of a commit, and the commit is denied
- **THEN** the reason holds `unit`, `failed`, the path of the log of that run, `lint`, `not run`, and the command `gatecub run`

#### Scenario: What a denial does not say

- **WHEN** any commit or push is denied
- **THEN** the reason holds neither `--no-verify` nor `observe` nor `uninstall`


#### Scenario: A failed check is fixed first

- **WHEN** `unit` failed on the content of a commit, and the commit is denied
- **THEN** the reason holds the line `gatecub: a check failed or timed out; first fix what its log shows.` before the command `gatecub run`; a denial of checks that were only not run holds no such line

#### Scenario: A program that cannot be started

- **WHEN** a check `gone` has `run = ["no-such-program-of-this-test"]`, `gatecub run` exits 5, and a commit of that content is denied
- **THEN** the line of `gone` in the reason says `failed` and that the program `no-such-program-of-this-test` could not be started

#### Scenario: The last run decided nothing

- **WHEN** `unit.sh` also writes `out.txt` into the working tree, `gatecub run` ends `unit` as `source_changed`, and a commit of the content it ran on is denied
- **THEN** the line of `unit` in the reason says `not run on this content (the last run ended source_changed: ` and names `out.txt`

#### Scenario: A change outside the checked paths is left unstaged

- **WHEN** every check of `gatecub.toml` has `paths`, a change of `src/app.txt` is staged, a change of `README.md` is not, and `git commit -m change` runs
- **THEN** the commit is denied, and the reason ends with the instruction to run `gatecub run` and names neither a path that differs nor another place to run it; after `gatecub run` exits 0, the same commit is recorded
### Requirement: Declaration change notice

When the tree a commit would record lacks a check that the tree of `HEAD` declares, or declares one with a different `run`, `paths` or `timeout`, the hook SHALL print a notice that lists each such check and what changed, with each list as `gatecub.toml` holds it, whether it allows or denies the commit.

Source: proposal, Open Question 4; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding U13.

#### Scenario: A check is removed

- **WHEN** the staged `gatecub.toml` no longer declares `unit`, `gatecub run` exits 0, and `git commit -a -m change` runs
- **THEN** the commit is recorded and standard error holds a notice that `unit` is no longer declared

#### Scenario: A check is added

- **WHEN** the staged `gatecub.toml` adds a check `build` and changes nothing else
- **THEN** the hook prints no such notice


#### Scenario: A changed check is shown as `gatecub.toml` holds it

- **WHEN** the staged `gatecub.toml` changes the `run` of `unit` to `["sh", "unit2.sh"]` and gives `lint` `paths = ["src/**"]`, and `git commit -a -m change` runs
- **THEN** standard error holds a notice that `run` of `unit` was `["sh", "unit.sh"]` and becomes `["sh", "unit2.sh"]`, and one that `paths` of `lint` was `not set` and becomes `["src/**"]`
### Requirement: Linked worktrees

The hooks that `gatecub init` installs SHALL apply in every linked worktree of the repository. A run recorded in one worktree SHALL count in another for the same checked content. Work in one worktree that is unfinished, unchecked or failing SHALL NOT change a decision about different content in another.

Source: review finding H9 (`docs/hooks.md:212-214`; a task in the main checkout blocks commits in every linked worktree).

#### Scenario: A failing main checkout does not block a worktree

- **WHEN** `src/app.txt` holds `broken` in the main checkout and `gatecub run` there exits 5, and in the linked worktree `../wt` `README.md` is edited, `gatecub run` exits 0, and `git commit -a -m docs` runs
- **THEN** the commit in `../wt` is recorded

#### Scenario: A worktree is gated too

- **WHEN** `src/app.txt` is edited in `../wt` and `git commit -a -m change` runs there without `gatecub run`
- **THEN** the commit is denied

### Requirement: Observe mode

`gatecub init --observe` SHALL put the repository's gate in observe mode, and `gatecub init` without that option SHALL return it to enforcing. In observe mode each hook SHALL record the decision it would have made, print it in one line, and allow the operation whatever that decision is, also when a cause prevents a decision.

Source: review finding H7 (no baseline exists for the workflow's benefit); proposal, Success measure 5; independent review of this draft, finding 12.

#### Scenario: A commit that would be denied goes through and is counted

- **WHEN** the gate is in observe mode, `src/app.txt` holds `broken`, `gatecub run` exits 5, and `git commit -a -m change` runs
- **THEN** the commit is recorded, standard error holds one line that says it would have been denied because `unit` failed, and `gatecub stats` counts one observed denial

#### Scenario: A cause that prevents a decision is observed too

- **WHEN** the gate is in observe mode, line 9 of the staged `gatecub.toml` is `tiemout = "5m"`, and `git commit -m change` runs
- **THEN** the commit is recorded and standard error holds one line that says it would have been denied because the declaration is invalid

#### Scenario: Enforcing again

- **WHEN** `gatecub init` runs after `gatecub init --observe`, and the same kind of commit is tried
- **THEN** the commit is denied

### Requirement: Removal

`gatecub uninstall` SHALL remove the hooks that `gatecub` wrote and the `gatecub/` store, and SHALL leave `gatecub.toml` and every hook file that `gatecub` did not write unchanged. In a directory that `core.hooksPath` names, only a file that is exactly the script of `gatecub` counts as written by `gatecub`. When a file it leaves still calls `gatecub hook`, `gatecub uninstall` SHALL name the file, print the line of it that holds the call, and exit 4.

Source: review finding L3 (`internal/codexinstall/codexinstall.go:142-160` removes links it does not own); independent review of this draft, finding 14; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding S5 and U14.

#### Scenario: Only `gatecub`'s own files go

- **WHEN** `pre-commit` and `pre-push` were written by `gatecub` and `gatecub uninstall` runs
- **THEN** it exits 0, both hooks and the `gatecub/` directory are gone, and `gatecub.toml` has the same SHA-256 as before

#### Scenario: Another tool's hook still calls `gatecub`

- **WHEN** `pre-commit` was written by another tool and calls `gatecub hook pre-commit`, `pre-push` was written by `gatecub`, and `gatecub uninstall` runs
- **THEN** `pre-push` and the `gatecub/` directory are gone, `pre-commit` has the same SHA-256 as before, and `gatecub uninstall` exits 4 and prints the path of `pre-commit` and the line to remove

#### Scenario: A hooks directory of another owner keeps what is not exactly `gatecub`'s

- **WHEN** `core.hooksPath` is `.githooks`, `.githooks/pre-push` is exactly the hook script of `gatecub`, `.githooks/pre-commit` holds the marker line of `gatecub`, a command of the team and `gatecub hook pre-commit "$@"`, and `gatecub uninstall` runs
- **THEN** `.githooks/pre-push` is gone, `.githooks/pre-commit` is unchanged, and `gatecub uninstall` exits 4 and names `.githooks/pre-commit` and the line to remove

#### Scenario: `gatecub uninstall` says what it removed

- **WHEN** `gatecub uninstall` runs after `gatecub init`
- **THEN** it exits 0 and prints `removed the hooks gatecub wrote, 3 in all, and the records of gatecub in` followed by the path of the `gatecub/` directory
