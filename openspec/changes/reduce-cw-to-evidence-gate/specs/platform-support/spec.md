# platform-support

## Purpose

Make the gate usable for any codebase on the three systems that developers
work on, and let a team install it without the toolchain of any programming
language.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- A git repository `portable` on branch `main` with one commit, holding
  `README.md` and this `cw.toml`:

  ```toml
  version = 1

  [[check]]
  id = "git"
  run = ["git", "--version"]
  ```

- `cw init` has run, so the hooks are installed and the gate enforces.
- "Windows" means Windows with Git for Windows. A Linux distribution that runs
  under the Windows Subsystem for Linux counts as Linux.
- "Each system" means Linux, macOS and Windows.
- "Denied" means the hook exits 3, git records nothing, and the reason is
  printed on standard error.

## ADDED Requirements

### Requirement: Supported systems

`cw` SHALL meet every requirement of this change on Linux, on macOS and on Windows with Git for Windows. Where a requirement names something that only some systems have, such as a signal or the executable mark of a file, its own text or scenario says what holds on the others.

Source: the owner's decision of 14 November 2026 (Windows is needed); proposal, Open Question 7; `docs/operations.md:131` (Windows is not supported today); `internal/proc/group_other.go` and `internal/fslock/fslock_other.go` (on other systems today only the program itself is stopped, and no file lock can be taken).

#### Scenario: A gated commit on each system

- **WHEN** on each system `README.md` is edited, and `cw run`, `git add -A` and `git commit -m docs` run in `portable`
- **THEN** `cw run` exits 0 and the commit is recorded

#### Scenario: A denied commit on each system

- **WHEN** on each system `README.md` is edited and `git commit -a -m docs` runs in `portable` without `cw run`
- **THEN** the commit is denied and the reason names `git` as not run on this content

### Requirement: Programs that a system lacks

When a check's program cannot be started, the run SHALL end `failed`, and the output SHALL name the program and say that it was not found or could not be started. `cw` SHALL NOT translate, replace or skip a check because of the system it runs on: one `cw.toml` holds the same checks on every system.

Source: `check-declaration`, Declaration file (nothing outside `cw.toml` changes a check); `check-evidence`, Outcome of a run (it names no outcome for a program that does not start); the owner's decision of 14 November 2026.

#### Scenario: A program that does not exist

- **WHEN** a check `ghost` has `run = ["cw-no-such-program"]` and `cw run ghost` runs
- **THEN** the run ends `failed`, the output names `cw-no-such-program` and says it was not found, and `cw run` exits 5

#### Scenario: A check written for another system

- **WHEN** on a Windows machine that has no `sh` on `PATH` a check has `run = ["sh", "unit.sh"]` and `cw run` runs
- **THEN** the run ends `failed` and the output says that `sh` was not found; `cw` does not look for another shell

### Requirement: Programs on Windows

On Windows `cw` SHALL find a check's program as the system does, so that a name without its extension finds a `.exe`, `.cmd` or `.bat` file. When the program is a batch file, each argument SHALL reach it unchanged, or `cw run` SHALL refuse that check, name the argument, and start nothing for it. An argument MUST NOT be read as a second command.

Source: `go doc os/exec.LookPath` and `go doc os/exec.Command`, Go 1.27.4 (a name needs an extension from `PATHEXT`; `cmd.exe`, "and thus, all batch files", unquote a command line differently from other programs); proposal, Open Question 9.

#### Scenario: A program is found without its extension

- **WHEN** on Windows a directory on `PATH` holds `hello.cmd`, a check has `run = ["hello"]`, and `cw run` runs
- **THEN** `hello.cmd` is executed

#### Scenario: Arguments reach a batch file as they were written

- **WHEN** on Windows `hello.cmd` passes its arguments on to a program that prints them, a check has `run = ["hello", "a b", "c&d"]`, and `cw run` runs
- **THEN** that program receives exactly the two arguments `a b` and `c&d`, or `cw run` exits 1 and names `c&d`; in neither case is a program named `d` started

### Requirement: Hooks on Windows

On Windows the hooks that `cw init` installs SHALL run in the shell that Git for Windows runs hooks in, and SHALL find the `cw` program that is on the user's `PATH`. A commit or a push SHALL get the same decision whether git is started from PowerShell, from `cmd.exe`, from Git Bash or by another program.

Source: `publish-gate`, Hook installation and Fail closed; the owner's decision of 14 November 2026. Not observed: every git observation under proposal, Sources was made on Linux.

#### Scenario: A commit from PowerShell and from the command prompt

- **WHEN** on Windows `README.md` is edited and `git commit -a -m docs` runs without `cw run`, once from PowerShell and once from `cmd.exe`
- **THEN** both commits are denied with the same reason

#### Scenario: A checked commit from PowerShell

- **WHEN** on Windows `README.md` is edited, and `cw run` and `git commit -a -m docs` run from PowerShell
- **THEN** the commit is recorded

### Requirement: Install without a toolchain

Each release SHALL provide, for Linux, macOS and Windows on amd64 and arm64, an archive that holds the `cw` program, and one file with the SHA-256 checksum of every archive. The first install instruction in `README.md` SHALL use them, SHALL check the archive against its checksum, and SHALL need no Go toolchain. A message that tells a user to install `cw` SHALL say where that instruction is and SHALL name no Go command.

Source: the owner's decision of 14 November 2026 (install without Go); review finding C1 (the release that `README.md:75-78` names does not exist); `README.md:65-66` (Go is listed as a requirement); `hooks/hooks.json` (the message a user gets says `go install`); `.goreleaser.yaml` (archives for darwin and linux only). The scenarios "`cw` is missing" (`publish-gate`) and "`cw` is not installed" (`agent-integration`) check the last sentence.

#### Scenario: Install on Linux and on macOS without Go

- **WHEN** on a machine with no Go toolchain the first install instruction of `README.md` for that system is executed
- **THEN** `cw version` prints the tag of the release

#### Scenario: Install on Windows without Go

- **WHEN** in PowerShell on a Windows machine with no Go toolchain the first install instruction of `README.md` for Windows is executed
- **THEN** `cw version` prints the tag of the release

#### Scenario: A damaged download is noticed

- **WHEN** one byte of the downloaded archive is changed before the checksum step of the instruction runs
- **THEN** that step fails and `cw` is not installed
