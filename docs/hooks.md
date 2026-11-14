# Hooks

`cw init` installs three git hooks. Each one asks `cw hook` whether git may
record or send the content, and `cw hook` answers from the records of earlier
runs. A hook executes no check and waits for none: `cw run` produces the
records before the commit. [Evidence](evidence.md) says what a record proves.

The examples on this page were produced on Linux with git 2.53. Temporary
paths are shortened to `<repository>`.

## What each hook judges

| Hook | git runs it | What `cw` judges |
| --- | --- | --- |
| `pre-commit` | before `git commit` records a commit, and when a conflicted `git merge`, `git cherry-pick` or `git revert` is continued | the tree of the index, which is the tree the commit records |
| `pre-merge-commit` | before `git merge` records a merge commit | the tree of the index, which is the tree of the merge commit |
| `pre-push` | before `git push` sends anything, also with `--dry-run` | the tree of the new tip of each ref the push updates |

A tree is allowed when it holds a valid `cw.toml` and every check it declares
has a recorded passing run on that tree's content. A tree whose `cw.toml`
declares no check is allowed.

### pre-commit and pre-merge-commit

`cw` writes the tree of the index from a copy of the index file, so the index
itself does not change. Inside the hook git points `GIT_INDEX_FILE` at the
index the commit will use, so the judged tree is the commit's tree whether it
was built from the index, by `git commit -a` or by `git commit <path>`.

When the commit is denied and the working tree holds other content than the
commit, the denial lists up to 20 paths that differ and says whether the
working tree as a whole is verified:

```text
cw: this commit is denied: its content has no recorded passing run of every declared check.
  lint: not run on this content
  The working tree holds other content than this commit. Paths that differ:
    src/new.txt
  The working tree as a whole is verified; this commit is not, because it records other content.
cw: `cw run` checks the working tree as it is on disk. Make the working tree hold exactly what you commit, by staging the rest or by setting it aside, and run:
  cw run
```

This is the case of a partial commit: `cw run` checked the whole working tree,
and the commit records only part of it. Stage everything, or set the rest
aside and run `cw run` again.

After a denied merge, the merge stays in progress. Run `cw run`, then
`git commit --no-edit` concludes it; `pre-commit` judges that commit.

When the tree of the commit lacks a check that the tree of `HEAD` declares, or
declares one with another `run`, `paths` or `timeout`, the hook prints a notice,
whether it allows or denies:

```text
cw: notice: this commit changes the required checks: `unit`: `run` was ["cat" "src/app.txt"] and becomes ["cat" "-v" "src/app.txt"]; `timeout` was 10m0s and becomes 5m0s
```

A check that is new gets no notice.

### pre-push

git gives the hook one line per ref the push updates. `cw` decides each line:

- A deletion of a ref is allowed.
- A tag is first peeled to the commit it names.
- An update of a branch adds nothing, and is allowed, when the commit the
  branch points at on the remote already contains the new tip. A branch that
  is new on the remote always adds something.
- An update of any other ref, such as a tag, adds nothing when a
  remote-tracking branch of the same remote, under `refs/remotes/<remote>/`,
  contains the commit. These are the refs of the last fetch in this clone.
- Otherwise the tree of the new tip must be verified. Only the tip is judged,
  not the commits below it.

One denied ref denies the whole push. A denial names the ref. When the working
tree does not hold the pushed commit, it says where to run `cw run`:

```text
cw: the push of refs/heads/f2 is denied: its content has no recorded passing run of every declared check.
  lint: not run on this content
cw: `cw run` checks the working tree, so it must run in a checkout of that commit, for example in a linked worktree:
  git worktree add --detach ../cw-check 2d251f00c5c4b2a2b18087b584fa5d60f640e2b1
and then, in that directory:
  cw run
```

### When a hook cannot decide

A hook that cannot establish that the content is verified denies, with exit
status 3, and names the cause:

| Cause | Example |
| --- | --- |
| `no_declaration` | the tree holds no `cw.toml` at its root, for example after `git rm cw.toml` |
| `invalid_declaration` | `cw.toml` breaks a rule; the denial prints the line, such as `cw.toml:9` |
| `tree` | the tree of the commit cannot be written, or a pushed ref names neither a commit nor a tree |
| `store` | the records in `cw/` cannot be read; the denial names the directory |
| `internal` | anything else, with git's own message |

A denial names each check that is not satisfied, with its state and the log
of the run that decided it, and ends with `cw run`. It names no way around the
gate.

### What a hook records

Each decision is appended as one line to `cw/decisions/<yyyy-mm>.jsonl`: the
time, the hook, the mode, the tree, the outcome (`allowed` or `denied`), the
ref of a push, and for a denial the state of each check that was not satisfied
or the cause. `cw stats` counts these lines. A line that cannot be written
prints a warning and changes no decision.

## The hook file

`cw init` writes the same short POSIX shell script for each of the three hooks,
with mode 0755. This is `pre-commit`:

```sh
#!/bin/sh
# cw-hook: written by `cw init`. `cw uninstall` removes this file.
if ! command -v cw >/dev/null 2>&1; then
  echo "cw: the cw program was not found on PATH, so this pre-commit cannot be judged and is denied." >&2
  echo "cw: the install instruction is at https://github.com/belevtsev/agent-workflows#install" >&2
  exit 3
fi
exec cw hook pre-commit "$@"
```

- The second line is the marker. A hook file that holds it was written by `cw`;
  `cw init` replaces it when its text differs or it cannot be executed, and
  `cw uninstall` removes it. A file without the marker belongs to another tool.
- The hooks are written into `hooks/` of the common git directory, the one git
  uses for every linked worktree.
- The script finds `cw` on `PATH` at the time git runs the hook. The decision
  is made by the installed `cw`, so a new `cw` takes effect without a new
  `cw init`.
- On Windows the code writes the same file; Git for Windows runs hooks in its
  own shell. This has not been run on Windows.

## When cw is not on PATH

The hook denies. git records or sends nothing, and the hook prints:

```text
cw: the cw program was not found on PATH, so this pre-commit cannot be judged and is denied.
cw: the install instruction is at https://github.com/belevtsev/agent-workflows#install
```

A clone with the hooks and without `cw` cannot commit or push until `cw` is
installed or the hooks are removed. [The README](../README.md#install) has the
install instruction.

## Observe mode

`cw init --observe` sets the gate to observe. Each hook then decides as usual,
records the decision, and allows the operation whatever the decision is,
also when a cause prevents a decision. A decision that would have denied
prints one line:

```text
cw: the gate only observes: this commit would have been denied because `lint` was not run on this content
```

`cw stats` counts these decisions under observe mode, apart from the enforced
ones. `cw init` without `--observe` sets the gate back to enforce. The mode is
the file `cw/mode` in the common git directory, so it holds for every linked
worktree, and it is never committed.

When `cw/mode` cannot be read, the hook denies with the cause `store`, also in
observe mode.

## Sharing a hook with another tool

`cw init` never changes a hook file it did not write.

- When `pre-commit`, `pre-merge-commit` or `pre-push` already exists without the
  marker line, `cw init` leaves it, installs the hooks that are free, and exits
  4. It prints the path of each file it left and the line to add:

  ```text
  cw: these hooks were written by another tool, and cw did not change them.
  cw: add the line shown to each hook. It must be the last command of that hook:
    <repository>/.git/hooks/pre-commit
        cw hook pre-commit "$@"
  ```

- Add that line as the last command of the other tool's hook. A command after
  it could change the files after `cw` judged them, and git would record a tree
  that `cw` did not judge.
- When the file can be executed and holds the text `cw hook <name>`, it counts
  as calling `cw`. `cw init` then exits 0, names the hook in the line
  `left as they are, because another tool wrote them and they call cw already`,
  and `cw status --json` reports `hooks` as `installed`.
- When `core.hooksPath` is set, the hooks belong to the owner of that directory
  or to a hook manager. `cw init` writes no hook at all, prints the line for
  each of the three hooks that does not call `cw` yet, and exits 4:

  ```text
  cw: core.hooksPath is set, so cw wrote no hook. That directory belongs to its owner.
  cw: add the line shown to each hook. It must be the last command of that hook:
    <repository>/.githooks/pre-commit
        cw hook pre-commit "$@"
  ```

- `cw uninstall` leaves such a file as it is. When it still calls `cw hook`,
  `cw uninstall` names it, prints the line to remove, and exits 4.

`cw hook <name>` gives the same decision when another hook calls it, as long as
it gets the arguments and the standard input git gave that hook: pass `"$@"`,
and for `pre-push` do not read standard input before `cw` does.

## Linked worktrees

A linked worktree, made with `git worktree add`, shares the common git
directory with the main checkout. So:

- the three hooks apply in every linked worktree;
- the records are shared: a run made in one worktree counts in another for the
  same checked content;
- unfinished, failing or unchecked work in one worktree changes no decision
  about different content in another, because each decision is about one tree.

When `core.hooksPath` is a relative path, git looks for the hooks relative to
each worktree. A worktree whose directory does not hold those hook files, for
example because they are not committed, is then not gated. `cw status` in that
worktree warns, and `cw status --json` reports `hooks` as `missing`.

## Known limits

- `git commit --no-verify` (also `-n`), `git merge --no-verify` and
  `git push --no-verify` skip the hooks. The push rule judges a commit that was
  made with `--no-verify` when it is pushed, unless the push skips the hook as
  well.
- A clone in which nobody ran `cw init` has no hooks and is not gated. git does
  not copy hooks with a clone. `cw status` and `cw context` say so. Continuous
  integration with `cw run --base` is the backstop; see
  [Operations](operations.md#continuous-integration).
- The design states, from observation on Linux, that git does not run
  `pre-commit` for a clean `git cherry-pick` or `git revert`, for `git rebase`
  (also when continued) or for a fast-forward merge. The push rule judges what
  they leave at the tip.
- The push rule judges the tip commit of each ref, not every commit below it.
  After a rebase or a cherry-pick of several commits, the commits below the tip
  were never judged one by one.
- git gives `pre-push` no sign of `--dry-run`, so a dry run is judged like a
  push.
- For a ref that is not a branch, "adds nothing" relies on the remote-tracking
  branches of this clone. A push to a URL instead of a named remote has none,
  so the tag is judged by its tree.
- A partial commit is denied when the rest of the working tree changes the
  checked content: the checks ran on the whole working tree.
- When another tool's hook changes files after `cw hook` ran, git records a
  tree that `cw` did not judge. Keep `cw hook` the last command; the push rule
  still judges the resulting commit.
- The gate trusts the git directory: its own records, the mode file, the hook
  files, the refs and the configuration. A process that writes there, or a
  person who edits a hook, can defeat it. The gate is a guardrail against a
  mistake, not a defence against someone who decides to go around it.
- On Windows, where the code treats every file as executable, the hook file
  and the commands of the agent plugin have not been run.

## Hooks of the agent plugin

The plugin for Claude Code and Codex adds hooks of the agent host. They do not
decide about commits or pushes; the git hooks do. The gate works without the
plugin. Each command first checks that `cw` is on `PATH`, and does nothing when
it is not, except at the start of a session.

| Host event | Claude Code (`hooks/hooks.json`) | Codex (`hooks/codex-hooks.json`) |
| --- | --- | --- |
| `SessionStart` | `cw context` | `cw context` |
| `PreToolUse` | `cw guard --host claude`, for the tools `Bash`, `Edit`, `Write`, `MultiEdit` | `cw guard --host codex`, for the tools `Bash`, `apply_patch` |
| `SubagentStop` | `cw tokens record --host claude` | `cw tokens record --host codex` |
| `SessionEnd` | `cw tokens record --host claude` | `cw tokens record --host codex` |

- At the start of a session, when `cw` is not on `PATH` and the root of the
  repository holds `cw.toml`, the command prints one line: that `cw` is not
  installed, that every commit and push will be denied by the git hooks, and
  where the install instruction is.
- `cw guard` asks the user in Claude Code, and denies with the reason in Codex,
  before a shell command whose text holds `--no-verify` and before an edit of
  `cw.toml`. It acts on no other call. See
  [Operations](operations.md#cw-guard).
- The token commands end with `|| true`, so a failure to record never fails the
  host's hook. [Tokens](tokens.md) says what they record.
- The hook commands of Claude Code are written for `bash`. Under Codex on
  Windows the file names `commandWindows`, which runs `cw` directly with no
  check for `PATH`, so the line that says `cw` is not installed is missing
  there. Neither has been run on Windows.
- `hooks/codex-hooks.json` is named in `.codex-plugin/plugin.json`. The plugin
  manifest of Claude Code, `.claude-plugin/plugin.json`, names no hooks file;
  Claude Code reads `hooks/hooks.json` of a plugin by default.
