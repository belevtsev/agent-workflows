# agent-integration

## Purpose

Tell a coding agent that the repository is gated and what to run, and stop it
from skipping the gate without the user's word, while the gate itself depends
on no agent host.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` of the `check-evidence` specification, with the hooks
  installed and the gate enforcing.
- "The plugin" means the plugin this project ships for Claude Code and for
  Codex.

## ADDED Requirements

### Requirement: The gate needs no plugin

The gate SHALL work with no plugin installed. The hooks SHALL judge a commit or a push in the same way whether an agent of any vendor, a script or a person runs git.

Source: review findings M1, M2 and M3 (the plugin hook is a second, weaker gate today); proposal, What Changes.

#### Scenario: A commit from a script

- **WHEN** no plugin is installed, `src/app.txt` holds `broken`, and a shell script runs `git commit -a -m change`
- **THEN** the commit is denied with the same reason an agent's commit gets

### Requirement: Session context

In a repository whose root holds `cw.toml`, the plugin SHALL add to the agent's context at the start of a session the text that `cw context` prints. That text SHALL say that commits and pushes are gated, that `cw run` produces the evidence, that `cw status` shows what is missing, and whether the hooks are installed. The session's directory SHALL be the one the host names in its notice. In any other directory the plugin SHALL add nothing.

Source: `hooks/hooks.json` (a session-start hook loads conventions today); review finding M24 (the agent-facing text is long); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), findings C7 and U6.

#### Scenario: A gated repository

- **WHEN** a session starts in `demo`
- **THEN** the agent's context holds `cw run` and `cw status`, in at most 1,000 characters

#### Scenario: Hooks are not installed in this clone

- **WHEN** a session starts in a fresh clone of `demo` in which `cw init` has not run
- **THEN** the context starts by saying that the hooks are not installed and that nothing is gated yet, and says that `cw init` installs them

#### Scenario: A repository without a declaration

- **WHEN** a session starts in a repository that has no `cw.toml`
- **THEN** the plugin adds nothing to the context

#### Scenario: The host names the session's directory

- **WHEN** the host starts the session-start command in a folder outside any repository, and its notice names `demo` as the session's directory
- **THEN** the context of `demo` is added; the other way round, nothing is added

#### Scenario: `cw context` in a terminal

- **WHEN** a user runs `cw context` without `--host` in `demo`, and its standard input never ends
- **THEN** `cw context` prints the context and returns without reading its input

### Requirement: Instruction text comes from `cw`

The plugin SHALL take the session text from the installed `cw`, and SHALL hold neither a copy of that text nor a `cw` version number. When `cw` is not on `PATH`, the plugin SHALL add one line that says so. The plugin SHALL look for `cw` only in the entries of `PATH` that are absolute paths, and SHALL block no tool call when `cw` fails.

Source: review finding C1 (README, both hook files and SKILL.md pin `cw@v3.0.0-rc.2`, which no release matches); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), findings S1, C1, X2 and X7.

#### Scenario: `cw` is upgraded

- **WHEN** a newer `cw` whose `cw context` prints different text is installed, the plugin is unchanged, and a session starts in `demo`
- **THEN** the context holds the text of the newer `cw`

#### Scenario: `cw` is not installed

- **WHEN** `cw` is not on `PATH` and a session starts in `demo`
- **THEN** the context holds one line that says `cw` is not installed and that commits in this repository will be denied, says where the install instruction is, and names no Go command

#### Scenario: A program named `cw` in the repository

- **WHEN** the root of `demo` holds an executable file `cw`, the first entry of `PATH` is empty or `.`, no other `cw` is on `PATH`, and a session starts or the agent is about to run a shell command
- **THEN** that file is not run, and each command of the plugin exits 0

#### Scenario: An older `cw`

- **WHEN** the `cw` on `PATH` prints its usage and exits 2 for every command it is given, and a session starts, the agent is about to run a shell command, a role ends or the session ends
- **THEN** each command of the plugin exits 0 and the host is neither asked nor told to deny

### Requirement: Ask before skipping

Before the agent runs a shell command in which a call of git skips the hooks or a call of `cw` turns the gate off, or changes `cw.toml`, a git hook, the git configuration or a record of `cw`, the plugin SHALL ask the user where the host can ask, and SHALL otherwise deny with the reason. It SHALL read the command as shell words, so that `--no-verify`, `-n` of `git commit` and a setting of `core.hooksPath` count only as arguments of git. The plugin SHALL NOT act on any other tool call.

Source: proposal, Open Questions 4 and 6; review finding M3 (Codex denies where Claude Code asks); `docs/other-tools.md`, row G-2 (matching a command as text is fragile in both directions); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), findings S4, S8, C3, X3 and A1.

#### Scenario: A commit that skips the hook

- **WHEN** the agent in Claude Code is about to run `git commit --no-verify -m change`
- **THEN** the user is asked, and the question says that the command skips the commit gate

#### Scenario: A change to the declaration

- **WHEN** the agent in Claude Code is about to edit `cw.toml` with its file-editing tool
- **THEN** the user is asked, and the question says that the edit changes the required checks

#### Scenario: The short form of the option

- **WHEN** the agent in Claude Code is about to run `git commit -n -m change`
- **THEN** the user is asked, and the question says that the command skips the commit gate

#### Scenario: A setting that replaces the hooks

- **WHEN** the agent in Claude Code is about to run `git -c core.hooksPath=/dev/null commit -m change`
- **THEN** the user is asked, and the question says that the command skips the commit gate

#### Scenario: The option as the text of an argument

- **WHEN** the agent is about to run `git commit -m "--no-verify is blocked"` or `grep -- --no-verify docs/hooks.md`
- **THEN** the plugin neither asks nor denies

#### Scenario: A command that cannot be read

- **WHEN** the agent in Claude Code is about to run a command that holds `--no-verify` and an opening quote that is never closed
- **THEN** the user is asked

#### Scenario: An edit of a hook or of a record

- **WHEN** the agent in Claude Code is about to change `.git/hooks/pre-commit` or a file under `.git/cw/` with its file-editing tool
- **THEN** the user is asked, and the question says what the edit changes

#### Scenario: An ordinary command

- **WHEN** the agent is about to run `go test ./...` or `git commit -m change`
- **THEN** the plugin neither asks nor denies

#### Scenario: The shell tool of Windows

- **WHEN** the agent in Claude Code is about to run `git commit --no-verify` with its `PowerShell` tool
- **THEN** the user is asked

#### Scenario: A call that turns the gate off

- **WHEN** the agent is about to run `cw uninstall` or `cw init --observe`
- **THEN** in Claude Code the user is asked and in Codex the call is denied, and the reason says that the command skips the commit gate

#### Scenario: An edit of the git configuration

- **WHEN** the agent in Claude Code is about to write `.git/config` with its file-editing tool
- **THEN** the user is asked, and the question names the git configuration

#### Scenario: A hook in the folder of `core.hooksPath`

- **WHEN** `core.hooksPath` is `.githooks` and the agent in Claude Code is about to edit `.githooks/pre-commit`
- **THEN** the user is asked, and the question names a git hook

#### Scenario: An edit through a link

- **WHEN** `alias.toml` is a symbolic link to `cw.toml` and the agent in Claude Code is about to write `alias.toml`
- **THEN** the user is asked, and the question names `cw.toml`

#### Scenario: A command given as a list

- **WHEN** the host gives the command as the list `["bash", "-lc", "git commit --no-verify"]` or `["git", "commit", "--no-verify"]`
- **THEN** in Claude Code the user is asked and in Codex the call is denied

#### Scenario: A notice larger than 16 MiB

- **WHEN** the agent in Claude Code is about to write `cw.toml` with a content of more than 16 MiB
- **THEN** the user is asked

#### Scenario: A patch given to the shell

- **WHEN** the agent is about to run `apply_patch` in the shell with a patch, given as a here-document, that updates `cw.toml`
- **THEN** in Claude Code the user is asked and in Codex the call is denied, and the reason names `cw.toml`

#### Scenario: An edit of another file

- **WHEN** the agent in Claude Code is about to write `README.md` with its file-editing tool
- **THEN** the plugin neither asks nor denies

### Requirement: The plugin never judges a commit or push

The plugin SHALL NOT allow or deny a `git commit` or `git push` because of the content it would record. Only the git hooks SHALL decide that, so the way a command is written in the shell SHALL NOT change the decision. The plugin reads a command for one purpose only, to ask before a call that skips the hooks; a call it does not recognise is not asked about, and the hooks decide as for any other.

Source: review findings M1, M2, M3 and H8 (`internal/gate/scan.go`, `internal/gate/command.go`, `internal/shellwords/shellwords.go:509`; three command shapes get past the parser and four of eight mutants survive its tests).

#### Scenario: A piped commit

- **WHEN** `src/app.txt` holds `broken` and the agent runs `git commit -q -a -m x 2>&1 | tail -5`
- **THEN** the commit is denied

#### Scenario: A commit inside another shell

- **WHEN** `src/app.txt` holds `broken` and the agent runs `/bin/bash -c 'git commit -a -m x'` or `/usr/bin/env git commit -a -m x`
- **THEN** the commit is denied

#### Scenario: A commit message that mentions a push

- **WHEN** the content is verified and the agent runs `git add -A && git commit -F -` with a heredoc message that holds the text `git push` between backticks
- **THEN** the commit is recorded and the user is asked nothing

### Requirement: Usage skill

The plugin SHALL ship one skill, `cw`, that tells an agent how to work in a gated repository: what to run before and after a change, what each denial and each outcome of a check means, and when the task workflow fits. The skill SHALL name only commands that `cw help` lists and no `cw` version number, and a test SHALL replay every example beside it against the `cw` of the same commit. The head of every skill of the plugin SHALL hold no key that a host rejects.

Source: the owner's statement of 14 November 2026 (a skill for an agent on how to use the tool for any task, with examples); `docs/other-tools.md`, Skills.

#### Scenario: An example shows output that `cw` does not print

- **WHEN** an example under `skills/cw/examples/` shows, for one of its commands, other output or another exit status than `cw` gives for it in the repository of the example
- **THEN** the tests fail and name the command

#### Scenario: A command that does not exist

- **WHEN** `skills/cw/SKILL.md` names `cw verify`
- **THEN** the tests fail, because `cw help` lists no such command

#### Scenario: An example the skill does not name

- **WHEN** a file is added under `skills/cw/examples/` and `skills/cw/SKILL.md` does not link it
- **THEN** the tests fail and name the file

#### Scenario: A head that a host rejects

- **WHEN** the head of a `SKILL.md` holds a key other than `name`, `description` and `metadata`
- **THEN** the tests fail and name the key
