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

In a repository whose root holds `cw.toml`, the plugin SHALL add to the agent's context at the start of a session the text that `cw context` prints. That text SHALL say that commits and pushes are gated, that `cw run` produces the evidence, that `cw status` shows what is missing, and whether the hooks are installed. In any other directory the plugin SHALL add nothing.

Source: `hooks/hooks.json` (a session-start hook loads conventions today); review finding M24 (the agent-facing text is long).

#### Scenario: A gated repository

- **WHEN** a session starts in `demo`
- **THEN** the agent's context holds `cw run` and `cw status`, in at most 1,000 characters

#### Scenario: Hooks are not installed in this clone

- **WHEN** a session starts in a fresh clone of `demo` in which `cw init` has not run
- **THEN** the context says that the hooks are not installed and that `cw init` installs them

#### Scenario: A repository without a declaration

- **WHEN** a session starts in a repository that has no `cw.toml`
- **THEN** the plugin adds nothing to the context

### Requirement: Instruction text comes from `cw`

The plugin SHALL take the session text from the installed `cw`, and SHALL hold neither a copy of that text nor a `cw` version number. When `cw` is not on `PATH`, the plugin SHALL add one line that says so.

Source: review finding C1 (README, both hook files and SKILL.md pin `cw@v3.0.0-rc.2`, which no release matches).

#### Scenario: `cw` is upgraded

- **WHEN** a newer `cw` whose `cw context` prints different text is installed, the plugin is unchanged, and a session starts in `demo`
- **THEN** the context holds the text of the newer `cw`

#### Scenario: `cw` is not installed

- **WHEN** `cw` is not on `PATH` and a session starts in `demo`
- **THEN** the context holds one line that says `cw` is not installed and that commits in this repository will be denied, says where the install instruction is, and names no Go command

### Requirement: Ask before skipping

Before the agent runs a shell command whose text holds `--no-verify`, or changes `cw.toml` with a file-editing tool, the plugin SHALL ask the user where the host can ask, and SHALL otherwise deny with the reason. The plugin SHALL NOT act on any other tool call.

Source: proposal, Open Questions 4 and 6; review finding M3 (Codex denies where Claude Code asks).

#### Scenario: A commit that skips the hook

- **WHEN** the agent in Claude Code is about to run `git commit --no-verify -m change`
- **THEN** the user is asked, and the question says that the command skips the commit gate

#### Scenario: A change to the declaration

- **WHEN** the agent in Claude Code is about to edit `cw.toml` with its file-editing tool
- **THEN** the user is asked, and the question says that the edit changes the required checks

#### Scenario: An ordinary command

- **WHEN** the agent is about to run `go test ./...` or `git commit -m change`
- **THEN** the plugin neither asks nor denies

### Requirement: The plugin never judges a commit or push

The plugin SHALL NOT allow, deny or interpret a `git commit` or `git push` command. Only the git hooks SHALL decide, so the way a command is written in the shell SHALL NOT change the decision.

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
