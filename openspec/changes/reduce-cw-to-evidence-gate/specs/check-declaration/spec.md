# check-declaration

## Purpose

Name the checks a repository requires before a commit, in one committed file
that works for any language and any agent, and that a person can read in a
minute.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- A git repository `demo` on branch `main` with this `cw.toml` at its root,
  committed:

  ```toml
  version = 1

  [[check]]
  id = "unit"
  run = ["sh", "unit.sh"]
  paths = ["src/**", "unit.sh"]

  [[check]]
  id = "lint"
  run = ["sh", "lint.sh"]
  ```

- "Refused" means the command exits 1, prints the reason, and writes nothing: it leaves no new or changed file or directory in the working tree, in the home directory or in the git directory. Objects that git stores while `cw` takes a snapshot of uncommitted content are not counted.

## ADDED Requirements

### Requirement: Declaration file

`cw` SHALL read the checks of a tree from the file `cw.toml` at the root of that tree and from no other place. No per-user file, environment variable or command-line option SHALL add, remove or change a check.

Source: proposal, What Changes (checks move from a per-task JSON plan to one committed file).

#### Scenario: The tree's own file governs

- **WHEN** `$HOME/.config/cw/cw.toml` exists and declares no checks, and `cw status` runs in `demo`
- **THEN** the output lists exactly the checks `unit` and `lint`

#### Scenario: Another branch has its own declaration

- **WHEN** branch `old`, whose `cw.toml` declares only `lint`, is checked out and `cw status` runs
- **THEN** the output lists exactly the check `lint`

### Requirement: Check definition

Each `[[check]]` SHALL have an `id` that is unique in the file and matches `[a-z0-9][a-z0-9_-]{0,39}`, and a `run` array of one or more strings. It MAY have `paths`, an array of one or more patterns, and `timeout`, a duration from `1s` to `6h` that defaults to `10m`. `cw` SHALL execute `run` as a program and its arguments, without a shell.

Source: proposal, Terms. A plan check is an argv today as well (review findings, H2 repro: `{"id":"slow","argv":["sleep","8"]}`).

#### Scenario: Arguments are not interpreted by a shell

- **WHEN** a check has `run = ["echo", "$HOME", "&&", "false"]` and `cw run` executes it
- **THEN** the run's outcome is `passed` and its log holds the line `$HOME && false`

#### Scenario: The timeout defaults to ten minutes

- **WHEN** the check `lint`, which has no `timeout`, runs a program that does not end
- **THEN** the run ends as `timed_out` ten minutes after it started

### Requirement: Path patterns

A `paths` pattern SHALL be matched against a path relative to the repository root, with `/` as the separator and with case respected. `*` SHALL match any run of characters inside one segment, `?` one character inside a segment, and `**` any number of whole segments, including none, so that `dir/**` also matches an entry named `dir`. A symbolic link and a submodule SHALL each be matched as one entry, by its own path.

Source: proposal, Terms ("same checked content"); independent review of this draft, finding 11.

#### Scenario: Double star crosses directories

- **WHEN** the pattern is `**/*.go`
- **THEN** it matches `main.go` and `a/b/c.go`, and does not match `main.go.bak`

#### Scenario: A directory prefix matches everything below it

- **WHEN** the pattern is `docs/**`
- **THEN** it matches `docs/a.md` and `docs/x/y.md`, and does not match `docs.md`

#### Scenario: A plain name matches at the root only

- **WHEN** the pattern is `go.mod`
- **THEN** it matches `go.mod` and does not match `tools/go.mod`

#### Scenario: A submodule is one entry

- **WHEN** the pattern is `vendor/lib/**` and `vendor/lib` is a submodule
- **THEN** it matches the entry `vendor/lib`, so a new submodule commit counts as a change inside `paths`

### Requirement: Invalid declaration

`cw` MUST refuse a declaration that is not valid TOML, whose `version` is not `1`, that holds a key `cw` does not know, or that breaks a rule of this specification. Each command other than a hook SHALL then exit 1 and print the file name, the line and the rule that is broken, and SHALL NOT run any check from that file. A hook SHALL deny, as `publish-gate` states.

Source: review finding L10 (a typo in a policy key is silent today, `internal/modelpolicy/modelpolicy.go:136-145`).

#### Scenario: A misspelt key is refused

- **WHEN** line 9 of `cw.toml` is `tiemout = "5m"` and `cw run` runs
- **THEN** it exits 1, the output holds `cw.toml:9` and `tiemout`, and no check is executed

#### Scenario: A duplicate id is refused

- **WHEN** `cw.toml` holds two checks with `id = "unit"` and `cw status` runs
- **THEN** it exits 1 and the output names `unit` and both line numbers

#### Scenario: A command written as one string is refused

- **WHEN** a check has `run = "go test ./..."` and `cw run` runs
- **THEN** it exits 1 and the output says that `run` must be an array of strings

### Requirement: Missing declaration

When the tree has no `cw.toml` at its root, `cw run` and `cw status` SHALL exit 1 and print that the repository has no declaration and that `cw init` creates one.

Source: proposal, Open Question 3.

#### Scenario: No file

- **WHEN** `cw.toml` is deleted from the working tree and `cw status` runs
- **THEN** it exits 1 and the output holds `cw.toml` and `cw init`

### Requirement: Create a declaration

`cw init` SHALL create `cw.toml` with `version = 1` and no checks when the repository root has none, and SHALL leave an existing `cw.toml` byte for byte unchanged. Outside a git repository it SHALL be refused.

Source: review finding H4 (the starter plan could not run the default flow); proposal, Success measure 1.

#### Scenario: A new repository

- **WHEN** `cw init` runs in a git repository without `cw.toml`
- **THEN** `cw.toml` exists, `cw status` exits 0, and its output says that no checks are declared

#### Scenario: An existing declaration is kept

- **WHEN** `cw init` runs in `demo`
- **THEN** the SHA-256 of `cw.toml` is the same before and after

### Requirement: Add a check from the command line

`cw add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]` SHALL append one `[[check]]` to `cw.toml`, with the words after `--` as `run` exactly as `cw` received them, and SHALL leave every earlier byte of the file unchanged. It SHALL be refused when the resulting file would be an invalid declaration.

Source: proposal, Success measure 1 (no hand-written file); review findings M1 to M3 (`cw` does not split a command line itself).

#### Scenario: A check is appended

- **WHEN** `cw add build --timeout 5m -- make -j4 build` runs in `demo`
- **THEN** `cw.toml` begins with its previous content, byte for byte, and ends with a check whose `id` is `build`, whose `run` is `["make", "-j4", "build"]` and whose `timeout` is `5m`

#### Scenario: A second check with the same id is refused

- **WHEN** `cw add unit -- true` runs in `demo`
- **THEN** it exits 1, names `unit`, and the SHA-256 of `cw.toml` is unchanged
