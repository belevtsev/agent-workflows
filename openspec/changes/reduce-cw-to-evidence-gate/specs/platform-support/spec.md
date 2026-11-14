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

Source: the owner's decision of 14 November 2026 (Windows is needed); proposal, Open Question 7; `docs/operations.md:131` (Windows is not supported today); `internal/proc/group_other.go` and `internal/fslock/fslock_other.go` (on other systems today only the program itself is stopped, and no file lock can be taken); second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), finding P3.

#### Scenario: A gated commit on each system

- **WHEN** on each system `README.md` is edited, and `cw run`, `git add -A` and `git commit -m docs` run in `portable`
- **THEN** `cw run` exits 0 and the commit is recorded

#### Scenario: A denied commit on each system

- **WHEN** on each system `README.md` is edited and `git commit -a -m docs` runs in `portable` without `cw run`
- **THEN** the commit is denied and the reason names `git` as not run on this content

#### Scenario: A name in another case

- **WHEN** on a file system that ignores the case of names, as on Windows and by default on macOS, the agent in Claude Code is about to edit `CW.TOML` or `.GIT/HOOKS/pre-commit` in `portable`
- **THEN** the user is asked, as for `cw.toml` or `.git/hooks/pre-commit`

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

On Windows `cw` SHALL find a check's program in the folders of `PATH` when `run` names it without a folder, and from the repository root otherwise, SHALL find a `.exe`, `.cmd` or `.bat` file by a name without its extension, and SHALL start the file by its full path. When the program is a batch file, each argument SHALL reach it unchanged, or `cw run` SHALL refuse that check, name the argument, and start nothing for it. An argument MUST NOT be read as a second command.

Source: `go doc os/exec.LookPath` and `go doc os/exec.Command`, Go 1.27.4 (a name needs an extension from `PATHEXT`; `cmd.exe`, "and thus, all batch files", unquote a command line differently from other programs); proposal, Open Question 9; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), findings P2 and P5.

#### Scenario: A program is found without its extension

- **WHEN** on Windows a directory on `PATH` holds `hello.cmd`, a check has `run = ["hello"]`, and `cw run` runs
- **THEN** `hello.cmd` is executed

#### Scenario: Arguments reach a batch file as they were written

- **WHEN** on Windows `hello.cmd` passes its arguments on to a program that prints them, a check has `run = ["hello", "a b", "c&d"]`, and `cw run` runs
- **THEN** that program receives exactly the two arguments `a b` and `c&d`, or `cw run` exits 1 and names `c&d`; in neither case is a program named `d` started

#### Scenario: A batch file named from a subfolder

- **WHEN** on Windows a check has `run = ["tools\\check.bat", "a&b"]` and `cw run` runs in a subfolder of `portable`
- **THEN** `cw run` refuses the check, says that it is a batch file and names `a&b`, and `tools\check.bat` is not started

#### Scenario: A batch file of the same name in the repository

- **WHEN** on Windows a folder on `PATH` holds `check.bat`, which prints `from path`, the root of `portable` holds another `check.bat`, which prints `from root`, a check has `run = ["check.bat"]`, and `cw run` runs at the root
- **THEN** the output holds `from path` and not `from root`

### Requirement: Hooks on Windows

On Windows the hooks that `cw init` installs SHALL run in the shell that Git for Windows runs hooks in, and SHALL find the `cw` program that is on the user's `PATH`. So SHALL the Windows commands of the Codex plugin, which SHALL run no program of the working directory, also not through a relative entry of `PATH`, and SHALL exit 0 (two exceptions follow). A commit or a push SHALL get the same decision whether git is started from PowerShell, from `cmd.exe`, from Git Bash or by another program.

The exceptions hold for the Windows commands of the Codex plugin. Behind an empty first entry of `PATH`, `cmd.exe` may run no `cw` of a later entry; the commands then act as they do without `cw`. And an entry of `PATH` that names a drive and no path from its root, such as `X:tools`, is read from the root of that drive when the working directory is on it and the Windows folder is not; it leads into the working directory when that directory is the root, or when the entry names it from the root.

Source: `publish-gate`, Hook installation and Fail closed; the owner's decision of 14 November 2026; second challenge of 14 November 2026 (`openspec/production-plan.md`, Independent challenge), findings S1, X2 and X10; `agent-integration`, Instruction text comes from `cw` (the line for a missing `cw`). Not observed: every git observation under proposal, Sources was made on Linux.

#### Scenario: A commit from PowerShell and from the command prompt

- **WHEN** on Windows `README.md` is edited and `git commit -a -m docs` runs without `cw run`, once from PowerShell and once from `cmd.exe`
- **THEN** both commits are denied with the same reason

#### Scenario: A checked commit from PowerShell

- **WHEN** on Windows `README.md` is edited, and `cw run` and `git commit -a -m docs` run from PowerShell
- **THEN** the commit is recorded

#### Scenario: A `cw` of the working directory

- **WHEN** on Windows `portable` holds `cw.bat`, no `cw` is on `PATH`, and Codex runs each Windows command of its plugin with `cmd.exe /C` in `portable`
- **THEN** each command exits 0 and does not run `cw.bat`

#### Scenario: A relative entry of `PATH` under Codex on Windows

- **WHEN** on Windows `portable` holds `cw.bat`, `git.bat`, `where.bat` and a folder `tools` with `cw.bat` and `git.bat`, the first entry of `PATH` is `.`, `tools` or `.\tools`, and Codex runs each Windows command of its plugin with `cmd.exe /C` in `portable`, once with no `cw` in another entry and once with the installed `cw` in a later one
- **THEN** each command exits 0 and runs none of these files, and with the installed `cw` the command before `git commit --no-verify` prints the decision `deny`

#### Scenario: A drive-relative entry of `PATH` under Codex on Windows

- **WHEN** on Windows `portable` is a folder, not the root, of a drive other than that of the Windows folder, it holds `cw.bat` and `tools\cw.bat`, the first entry of `PATH` names that drive and no path from its root, as `X:tools`, `X:` or `X:.`, no other entry holds `cw`, and Codex runs each Windows command of its plugin with `cmd.exe /C` in `portable`
- **THEN** each command exits 0 and runs none of these files

#### Scenario: The guard of Codex on Windows

- **WHEN** on Windows `cw` is on `PATH` and Codex runs the Windows command of its plugin before the agent runs `git commit --no-verify`
- **THEN** the command prints the decision `deny`

#### Scenario: An older `cw` on Windows

- **WHEN** on Windows the `cw` first on `PATH` is a batch file that exits 2, and Codex runs each Windows command of its plugin
- **THEN** each command exits 0

#### Scenario: Codex starts a session on Windows without `cw`

- **WHEN** on Windows no `cw` is on `PATH` and Codex runs the Windows command of its plugin for the start of a session, once in the root of `portable` and once in a folder outside any repository
- **THEN** in the root of `portable` the command prints one line that says that `cw` is not installed and where the install instruction is, outside a repository it prints nothing, and both exit 0

#### Scenario: A file named `cw` that Windows cannot run

- **WHEN** on Windows the only `cw` on `PATH` is a file of that name without an extension, and Codex runs the Windows command of its plugin for the start of a session in the root of `portable`
- **THEN** the command prints the line that says that `cw` is not installed, and exits 0

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
