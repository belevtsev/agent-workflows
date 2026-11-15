# Hooks

`gatecub init` installs three git hooks. Each one asks `gatecub hook` whether git may
record or send the content, and `gatecub hook` answers from the records of earlier
runs. A hook executes no check and waits for none: `gatecub run` produces the
records before the commit. [Evidence](evidence.md) says what a record proves.

<img src="diagrams/gate.svg" alt="How the gate decides. First the evidence: gatecub run runs what has not passed and records each run. Then the decision, in enforce mode: git commit or git push starts one of three git hooks, the hook asks gatecub hook, and gatecub hook judges the commit or the pushed tip against the records. When every check passed on exactly this content, git goes on. When not, or when gatecub hook cannot tell, the commit or push is denied and says why: fix, then run again." width="100%">

The picture shows the gate in enforce mode, as `gatecub init` sets it. [Observe mode](#observe-mode)
allows every operation, and [pre-push](#pre-push) lists the pushes that are allowed without a
judgement.

The examples on this page were produced on Linux with git 2.53. Temporary
paths are shortened to `<repository>`.

## What each hook judges

| Hook | git runs it | What `gatecub` judges |
| --- | --- | --- |
| `pre-commit` | before `git commit` records a commit, and when a conflicted `git merge`, `git cherry-pick` or `git revert` is continued | the tree of the index, which is the tree the commit records |
| `pre-merge-commit` | before `git merge` records a merge commit | the tree of the index, which is the tree of the merge commit |
| `pre-push` | before `git push` sends anything, also with `--dry-run` | the tree of the new tip of each ref the push updates |

A tree is allowed when it holds a valid `gatecub.toml` and every check it declares
has a recorded passing run on that tree's content. A tree whose `gatecub.toml`
declares no check is allowed.

### pre-commit and pre-merge-commit

`gatecub` writes the tree of the index from a copy of the index file, so the index
itself does not change. Inside the hook git points `GIT_INDEX_FILE` at the
index the commit will use, so the judged tree is the commit's tree whether it
was built from the index, by `git commit -a` or by `git commit <path>`.

When the commit is denied and the working tree holds other checked content
than the commit for a check that is not satisfied, the denial lists up to 20
paths that differ and says whether the working tree as a whole is verified:

```text
gatecub: this commit is denied: its content has no recorded passing run of every declared check.
  lint: not run on this content
  The working tree holds other content than this commit. Paths that differ:
    src/new.txt
  The working tree as a whole is verified; this commit is not, because it records other content.
gatecub: `gatecub run` checks the working tree as it is on disk. Make the working tree hold exactly what you commit, by staging the rest (git add -A) or by setting it aside (git stash push --keep-index --include-untracked), and run:
  gatecub run
```

This is the case of a partial commit: `gatecub run` checked the whole working tree,
and the commit records only part of it. Stage everything with `git add -A`, or
set the rest aside with `git stash push --keep-index --include-untracked`, run
`gatecub run` again, commit, and bring the rest back with `git stash pop`.

When a check that is not satisfied failed or timed out, the denial adds the
line `gatecub: a check failed or timed out; first fix what its log shows.` before
the command.

After a denied merge, the merge stays in progress. Run `gatecub run`, then
`git commit --no-edit` concludes it; `pre-commit` judges that commit.

When the tree of the commit lacks a check that the tree of `HEAD` declares, or
declares one with another `run`, `paths` or `timeout`, the hook prints a notice,
whether it allows or denies:

```text
gatecub: notice: this commit changes the required checks: `unit`: `run` was ["cat", "src/app.txt"] and becomes ["cat", "-v", "src/app.txt"]; `timeout` was 10m0s and becomes 5m0s
```

A list is printed as `gatecub.toml` holds it, and an absent `paths` as `not set`. A
check that is removed gets the line
``gatecub: notice: this commit changes the required checks: `lint` is no longer declared``.
A check that is new gets no notice.

### pre-push

git gives the hook one line per ref the push updates. `gatecub` decides each line:

- A deletion of a ref is allowed.
- A ref under `refs/notes/` is allowed and not judged: notes hold no content of
  the project.
- A tag is first peeled to the commit it names.
- An update of a branch adds nothing, and is allowed, when the commit the
  branch points at on the remote already contains the new tip. A branch that
  is new on the remote always adds something.
- An update of any other ref, such as a tag, adds nothing when the new tip of a
  branch that the same push updates contains the commit, or when a
  remote-tracking branch of the same remote, under `refs/remotes/<remote>/`,
  contains it. The remote-tracking branches are the refs of the last fetch in
  this clone.
- Otherwise the tree of the new tip must be verified. Only the tip is judged,
  not the commits below it.

One denied ref denies the whole push. A denial names the ref. When the working
tree holds the same checked content as the pushed commit for every check that
is not satisfied, the denial ends with `gatecub run`, to be run here. Otherwise it
says where to run `gatecub run`:

```text
gatecub: the push of refs/heads/f2 is denied: its content has no recorded passing run of every declared check.
  lint: not run on this content
gatecub: `gatecub run` checks the working tree, so it must run in a checkout of that commit, for example in a linked worktree:
  git worktree add --detach ../gatecub-check 2d251f00c5c4b2a2b18087b584fa5d60f640e2b1
and then, in that directory:
  gatecub run
```

### When a hook cannot decide

A hook that cannot establish that the content is verified denies, with exit
status 3, and names the cause:

| Cause | Example |
| --- | --- |
| `no_declaration` | the tree holds no `gatecub.toml` at its root, for example after `git rm gatecub.toml`; the denial names [A tree without gatecub.toml](operations.md#a-tree-without-gatecubtoml) |
| `invalid_declaration` | `gatecub.toml` breaks a rule; the denial prints the line, such as `gatecub.toml:9` |
| `tree` | the tree of the commit cannot be written, or a pushed ref names neither a commit nor a tree |
| `store` | the records in `gatecub/` cannot be read; the denial names the directory |
| `internal` | anything else, with git's own message |

A denial names each check that is not satisfied, with its state and the log
of the run that decided it, and ends with `gatecub run`. When the last run on this
content decided nothing, the denial names how it ended, such as
`(the last run ended source_changed: ...)`. It names no way around the gate.

### What a hook records

Each decision is appended as one line to `gatecub/decisions/<yyyy-mm>.jsonl`: the
time, the hook, the mode, the tree, the outcome (`allowed` or `denied`), the
ref of a push, and for a denial the state of each check that was not satisfied
or the cause. `gatecub stats` counts these lines. A line that cannot be written
prints a warning and changes no decision.

## The hook file

`gatecub init` writes the same short POSIX shell script for each of the three hooks,
with mode 0755. This is `pre-commit`:

```sh
#!/bin/sh
# gatecub-hook: written by `gatecub init`. `gatecub uninstall` removes this file.
dirs=; IFS=:; set -f
for dir in $PATH; do case "$dir" in /*) dirs="${dirs:+$dirs:}$dir" ;; esac; done; unset IFS; set +f
gatecub=$([ -n "$dirs" ] && PATH=$dirs && command -v gatecub 2>/dev/null)
case "$gatecub" in /*) exec "$gatecub" hook pre-commit "$@" ;; esac
echo "gatecub: the gatecub program was not found in an entry of PATH that is an absolute path, so this pre-commit cannot be judged and is denied. An empty or relative entry is not searched." >&2
echo "gatecub: the install instruction is at https://github.com/belevtsev/gatecub#install" >&2
exit 3
```

- The second line is the marker. A hook file that holds it was written by `gatecub`;
  `gatecub init` replaces it when its text differs or it cannot be executed. A file
  without the marker belongs to another tool.
- The script looks for `gatecub` only in the entries of `PATH` that are absolute
  paths. An empty or a relative entry, such as `.`, would let the hook run a
  file named `gatecub` from the repository, so the script leaves such entries out
  before it asks the shell where `gatecub` is. It does not go by the form of the
  shell's answer: the `sh` of macOS answers with an absolute path also for a
  file it found through `.`. When no absolute entry holds `gatecub`, the script
  denies and names the install instruction.
- The hooks are written into `hooks/` of the common git directory, the one git
  uses for every linked worktree.
- The script finds `gatecub` on `PATH` at the time git runs the hook. The decision
  is made by the installed `gatecub`, so a new `gatecub` takes effect without a new
  `gatecub init`.
- On Windows the code writes the same file; Git for Windows runs hooks in its
  own shell. The tests run it there in CI, with git started from PowerShell
  and from `cmd.exe`. Nobody has tried it there by hand.

## When gatecub is not on PATH

The hook denies. git records or sends nothing, and the hook prints:

```text
gatecub: the gatecub program was not found in an entry of PATH that is an absolute path, so this pre-commit cannot be judged and is denied. An empty or relative entry is not searched.
gatecub: the install instruction is at https://github.com/belevtsev/gatecub#install
```

It prints the same when a `gatecub` could be found only through an empty or
relative entry: with `PATH=:/usr/bin:/bin` and an executable file `gatecub` in the
root of the repository, that file is not run.

A clone with the hooks and without `gatecub` cannot commit or push until `gatecub` is
installed or the hooks are removed. [The README](../README.md#install) has the
install instruction.

## Observe mode

`gatecub init --observe` sets the gate to observe. Each hook then decides as usual,
records the decision, and allows the operation whatever the decision is,
also when a cause prevents a decision. A decision that would have denied
prints one line:

```text
gatecub: the gate only observes: this commit would have been denied because `lint` was not run on this content
```

`gatecub stats` counts these decisions under observe mode, apart from the enforced
ones. `gatecub init` without `--observe` sets the gate back to enforce. The mode is
the file `gatecub/mode` in the common git directory, so it holds for every linked
worktree, and it is never committed.

When `gatecub/mode` cannot be read, the hook denies with the cause `store`, also in
observe mode.

## Sharing a hook with another tool

`gatecub init` never changes a hook file it did not write.

- When `pre-commit`, `pre-merge-commit` or `pre-push` already exists without the
  marker line, `gatecub init` leaves it, installs the hooks that are free, and exits
  4. It prints the path of each file it left and the line to add:

  ```text
  gatecub: these hooks were written by another tool, and gatecub did not change them.
  gatecub: add the line shown to each hook. It must be the last command of that hook:
    <repository>/.git/hooks/pre-commit
        gatecub hook pre-commit "$@"
  ```

- Add that line as the last command of the other tool's hook. A command after
  it could change the files after `gatecub` judged them, and git would record a tree
  that `gatecub` did not judge.
- When the file can be executed and a line of it that is no comment holds
  `gatecub hook <name>`, it counts as calling `gatecub`. A line that starts with `#` does
  not count. `gatecub init` then exits 0, names the hook in the line
  `left as they are, because another tool wrote them and they call gatecub already`,
  and `gatecub status --json` reports `hooks` as `installed`.
- When `core.hooksPath` is set, the hooks belong to the owner of that directory
  or to a hook manager. `gatecub init` writes no hook at all, prints the line for
  each of the three hooks that does not call `gatecub` yet, and exits 4:

  ```text
  gatecub: core.hooksPath is set, so gatecub wrote no hook. That directory belongs to its owner.
  gatecub: add the line shown to each hook. It must be the last command of that hook:
    <repository>/.githooks/pre-commit
        gatecub hook pre-commit "$@"
  ```

- `gatecub uninstall` removes, in `hooks/` of the common git directory, each of the
  three files that holds the marker of `gatecub`. In a directory that
  `core.hooksPath` names, it removes only a file that is exactly the script
  `gatecub` writes, and leaves every other file, also one with the marker that
  someone edited. When a file it leaves still calls `gatecub hook`, `gatecub uninstall`
  names it, prints the line to remove, and exits 4.

`gatecub hook <name>` gives the same decision when another hook calls it, as long as
it gets the arguments and the standard input git gave that hook: pass `"$@"`,
and for `pre-push` do not read standard input before `gatecub` does.

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
example because they are not committed, is then not gated. `gatecub status` in that
worktree warns, and `gatecub status --json` reports `hooks` as `missing`.

## Known limits

- `git commit --no-verify` (also `-n`), `git merge --no-verify` and
  `git push --no-verify` skip the hooks. The push rule judges a commit that was
  made with `--no-verify` when it is pushed, unless the push skips the hook as
  well.
- Some tools commit without the hooks, or replace the hooks directory, without
  anyone asking for it. Reports name Aider, which passes `--no-verify` by
  default; two versions of Cursor, which pointed `core.hooksPath` elsewhere;
  the creation of a worktree by Claude Code, which wrote `core.hooksPath` into
  the shared configuration; and an action that commits through the hosting
  service and runs no local hook. [What other tools taught us](other-tools.md)
  has the sources. `gatecub status` warns when the hooks git runs in a clone do not
  call `gatecub`. A setting given on one command line it cannot see.
- A clone in which nobody ran `gatecub init` has no hooks and is not gated. git does
  not copy hooks with a clone. `gatecub status` and `gatecub context` say so. Continuous
  integration with `gatecub run --base` is the backstop; see
  [Operations](operations.md#continuous-integration).
- The design states, from observation on Linux, that git does not run
  `pre-commit` for a clean `git cherry-pick` or `git revert`, for `git rebase`
  (also when continued) or for a fast-forward merge. The push rule judges what
  they leave at the tip.
- `git am` runs `pre-applypatch`, not `pre-commit`. A commit made with
  `git commit-tree` and moved onto a branch with `git update-ref` runs no hook
  of a commit; `git update-ref` runs `reference-transaction`, which `gatecub` does
  not install. In both cases the push hook still judges the tip that is
  pushed. `scenarios/gitfacts` holds this git behaviour as tests.
- A file that another process stages while `pre-commit` runs is committed
  without being judged: git does not hold the index during the hook and reads
  it again after the hook. A hook cannot prevent this. The push hook still
  judges the tip that is pushed.
- A forced push that moves a branch back to a commit the remote's branch
  already contains is allowed, and nothing is judged: the commit is already on
  the remote.
- The push hook does not judge a ref under `refs/notes/`, nor a ref that is no
  branch, such as a tag, whose commit the new tip of a branch of the same push
  contains. That branch is judged.
- A tree without `gatecub.toml` cannot be committed or pushed, also when that is on
  purpose. [A tree without gatecub.toml](operations.md#a-tree-without-gatecubtoml) says
  what to do when a project stops using `gatecub`, and for a branch with other
  content.
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
- When another tool's hook changes files after `gatecub hook` ran, git records a
  tree that `gatecub` did not judge. Keep `gatecub hook` the last command; the push rule
  still judges the resulting commit.
- The gate trusts the git directory: its own records, the mode file, the hook
  files, the refs and the configuration. A process that writes there, or a
  person who edits a hook, can defeat it. The gate is a guardrail against a
  mistake, not a defence against someone who decides to go around it.
- On Windows the code treats every file as executable. There the tests in CI
  run the hook file and the Windows commands of the Codex plugin, and skip
  the plugin commands that are written for `sh`. Nobody has tried any of it
  there by hand, and no agent host was started there.

## Hooks of the agent plugin

The plugin for Claude Code and Codex adds hooks of the agent host. They do not
decide about commits or pushes; the git hooks do. The gate works without the
plugin. Each command keeps only the entries of `PATH` that are absolute paths,
as the hook file does, for all it runs: `gatecub`, and `git` at the start of a
session. A file `gatecub` that is not executable counts as none. When no entry
holds `gatecub`, the command does nothing; at the start of a session it prints the
one line described below, unless `PATH` holds no absolute entry at all. Each
command always exits 0, so a missing or an older `gatecub` blocks no tool call.
Codex starts the `sh` that runs its commands; where that `sh` comes from is
not in the hands of the plugin.

| Host event | Claude Code (`hooks/hooks.json`) | Codex (`hooks/codex-hooks.json`) |
| --- | --- | --- |
| `SessionStart` | `gatecub context --host claude` | `gatecub context --host codex` |
| `PreToolUse` | `gatecub guard --host claude`, for the tools `Bash`, `PowerShell`, `Edit`, `Write`, `MultiEdit` | `gatecub guard --host codex`, for the tools `Bash`, `apply_patch` |
| `SubagentStop` | `gatecub tokens record --host claude` | `gatecub tokens record --host codex` |
| `SessionEnd` | `gatecub tokens record --host claude` | `gatecub tokens record --host codex` |

- At the start of a session, when `gatecub` is not on `PATH` and the root of the
  repository holds `gatecub.toml`, the command prints one line: that `gatecub` is not
  installed, that every commit and push will be denied by the git hooks, and
  where the install instruction is.
- `gatecub guard` asks the user in Claude Code, and denies with the reason in Codex,
  before a shell command with a git call that skips git's hooks or a `gatecub` call
  that turns the gate off, and before an edit of `gatecub.toml`, of a git hook, of
  the git configuration or of a record of `gatecub`. It reads the command as shell
  words, not as text. It is a reminder for an agent; the git hooks decide. See
  [Operations](operations.md#gatecub-guard), which also lists what it misses.
- A failure to record tokens never fails the host's hook.
  [Tokens](tokens.md) says what the token commands record.
- The hook commands of Claude Code are written for `bash`; those of Codex are
  wrapped in `sh -c`. Under Codex on Windows the file names `commandWindows`
  for each event. It sets `NoDefaultCurrentDirectoryInExePath=1`, so that
  `cmd.exe` does not look for `gatecub` in the working directory. Before it runs
  `gatecub` it goes to the root of the session's drive and then to the Windows
  folder, so that a relative entry of `PATH`, such as `.` or `D:tools`,
  leads there and not into the repository. `gatecub` takes the session's
  directory from the `cwd` of the host's notice. The command holds no double
  quote and ends with `exit /b 0`. At the start of a session it runs
  `gatecub context`; when `cmd.exe` finds no `gatecub` it can run (exit status 9009),
  the command runs no other program and prints the line when the session's
  directory itself holds `gatecub.toml`, so a session started in a subfolder gets
  no line there. Two cases are not covered. An entry that names the session's
  drive and no path from its root, such as `D:tools`, is read from the root
  of that drive when the Windows folder is on another drive: it leads into
  the repository when the repository is that root, or when the entry names
  the repository's folder from the root. And behind an empty first entry
  of `PATH`, `cmd.exe` ran no `gatecub` of a later entry on the Windows image of
  the CI in November 2026: with such a `PATH` the commands before a tool call
  and for the tokens do nothing, and the command at the start of a session
  prints the line that says `gatecub` is not installed, although it is installed.
  Tests run these Windows commands with `cmd.exe /C` on Windows in CI. The
  commands for `bash` and `sh` are tested on Linux and macOS only: on Windows
  the hosts run them in Git Bash, which the tests do not set up.
- `hooks/codex-hooks.json` is named in `.codex-plugin/plugin.json`. The plugin
  manifest of Claude Code, `.claude-plugin/plugin.json`, names no hooks file;
  Claude Code reads `hooks/hooks.json` of a plugin by default.
