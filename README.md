# cw

`cw` is one small program for any git repository, in any language. It does
three things:

- **A gate.** A commit or a push is allowed only when every check the
  repository declares in `cw.toml` has a recorded passing run on exactly that
  content. Git hooks enforce it, for people and for coding agents alike.
- **Roles.** A coding agent takes a task from a specification to tested code
  through seven roles, each with one job, its own model and a limit on what it
  may change. `cw` checks afterwards what a program can check.
- **A token count.** `cw tokens` says what a task cost, role by role, and how
  much of it the workflow itself used.

The gate is a guardrail, not a sandbox: it stops a mistake, and it does not
stop a person who decides to go around it.

## State

- This is a pre-release:
  [`v3.0.0-beta.1`](https://github.com/belevtsev/agent-workflows/releases/tag/v3.0.0-beta.1),
  published on 15 November 2026. The release workflow followed the first
  install instruction below against it on Linux, macOS and Windows. CI builds
  the same six archives from every commit and follows the instruction with
  them.
- **Nothing here is measured to improve anything.** The gate has not yet been
  used on real work. The plugin and the roles ran with a model in scratch
  repositories only, on two small tasks:
  [benchmarks](docs/benchmarks.md) says what that showed and what it cost.
  The [production plan](openspec/production-plan.md) says what was built and
  how each part was checked.
- It was built and tried by hand on Linux. Its tests pass in CI on Linux,
  macOS and Windows; nothing was tried by hand on macOS or Windows.

## Install

`cw` is one file, and it needs git. It uses options that git has had since
version 2.24, and was tried by hand with git 2.53 only. On Windows the git
hooks are shell scripts, run by the `sh` of Git for Windows.

**From a release, without Go.** The archive is checked against `SHA256SUMS`
before anything is unpacked.

Linux and macOS:

```sh
tag=v3.0.0-beta.1
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
base=https://github.com/belevtsev/agent-workflows/releases/download/$tag
curl -fsSLO "$base/cw_${tag}_${os}_${arch}.tar.gz"
curl -fsSLO "$base/SHA256SUMS"
grep " cw_${tag}_${os}_${arch}.tar.gz\$" SHA256SUMS | (sha256sum -c - 2>/dev/null || shasum -a 256 -c -) &&
  tar -xzf "cw_${tag}_${os}_${arch}.tar.gz" cw &&
  mkdir -p "$HOME/.local/bin" && mv cw "$HOME/.local/bin/cw"
```

Windows, in PowerShell:

```powershell
$tag = "v3.0.0-beta.1"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$base = "https://github.com/belevtsev/agent-workflows/releases/download/$tag"
$zip = "cw_${tag}_windows_${arch}.zip"
Invoke-WebRequest "$base/$zip" -OutFile $zip
Invoke-WebRequest "$base/SHA256SUMS" -OutFile SHA256SUMS
$want = (Select-String -Path SHA256SUMS -Pattern " $zip$").Line.Split(" ")[0]
if ((Get-FileHash $zip -Algorithm SHA256).Hash -ne $want) { throw "the checksum of $zip does not match" }
Expand-Archive $zip -DestinationPath "$env:LOCALAPPDATA\cw" -Force
```

Then put the directory that holds `cw` on your `PATH`, and open a new shell.
`$HOME/.local/bin` is not on `PATH` by default on macOS, nor on some Linux
systems. For bash, or a shell that reads `~/.profile`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile
```

For zsh, the shell of macOS, write the same line to `~/.zprofile`. On Windows,
in PowerShell, for your user:

```powershell
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$env:LOCALAPPDATA\cw", "User")
```

The hooks look for `cw` only in the entries of `PATH` that are absolute paths; see
[hooks](docs/hooks.md#when-cw-is-not-on-path).

**With Go.** `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-beta.1`

**From this branch, today.** Building from source needs Go 1.27. In the root
of this checkout:

```sh
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/cw" ./cmd/cw
```

Do not leave a built `cw` in the root of a repository: git does not ignore it,
so it becomes part of the content every check is judged on.
[The test guide](docs/try-it.md) builds it into a folder of its own.

## The five commands

In a repository of yours:

```sh
cw init                          # creates cw.toml, installs three git hooks
cw add test -- <your test command>
cw run                           # runs the checks and records the result
git add -A
git commit -m "..."              # allowed: the content has a passing run
```

Change a file and commit again without `cw run`: the commit is denied, and the
message names the check that has no passing run and the command to run. The
[quick start](docs/quickstart.md) is the same five commands as a text that a
test executes.

## For a coding agent

The gate needs no plugin: the git hooks decide. The plugin in this repository
adds three small things on Claude Code and on Codex. It tells a new session
what the gate requires; it stops a git call that skips the hooks, such as
`git commit --no-verify` or `git commit -n`, a `cw` call that turns the gate
off, and an edit of `cw.toml`, a hook file, the git configuration or a record
of `cw`; and it gives `cw tokens record` the notices that a role or the session
has ended. Claude Code asks you before such a call. Codex refuses it and gives
the reason, because Codex cannot ask from a hook; run the call yourself if you
want it. The guard is a reminder, not a wall:
[operations](docs/operations.md#what-it-misses) lists what it misses.

It ships two skills. [`cw`](skills/cw/SKILL.md) tells an agent how to work in
a gated repository: the loop for any change, what each denial means, and
seven worked examples that a test replays against `cw`. `task` is the role
workflow below. On Claude Code the plugin also ships the seven roles as
agents; on Codex a role is started from the text `cw roles text <role>`
prints. Nothing was run on Codex.

On Claude Code:

```text
/plugin install agent-workflows --marketplace belevtsev/agent-workflows
```

On Codex, add this repository as a marketplace with
`codex plugin marketplace add belevtsev/agent-workflows`, then add the plugin
`agent-workflows` with `codex plugin add`. Codex does not run the hooks of a
plugin when it installs it. Its documentation says: "Installing or enabling a
plugin doesn't automatically trust its hooks … Codex skips them until the user
reviews and trusts the current hook definition." Until you review and trust
them, the session gets no text from `cw context`, no guard and no token
records. The git hooks still decide.

## The roles

`cw roles` prints the seven roles: `lookup`, `spec-writer`, `architect`,
`quality-engineer`, `developer`, `reviewer` and `git`, each with its model, its
effort and what it may write. A repository changes the models in
`cw-roles.toml`. The texts name no language and no build tool; the commands of
a repository come from its `cw.toml`.

- [Roles](docs/roles.md): what each role does and may change.
- [The task workflow](docs/workflow.md): the two paths, the record of a task
  under `openspec/changes/<task>/`, and what `cw change` checks.
- [Tokens](docs/tokens.md): what is counted, and what "workflow tokens" mean.

Whether seven roles give better code than one agent working alone is **not
measured**. The roles cost tokens that one agent would not spend, and
`cw tokens` shows how many. [The evaluation](docs/evaluation.md) says how the
comparison is to be made; it has not been run. A first run on two small tasks
is in [benchmarks](docs/benchmarks.md): it shows what the roles cost there,
not whether the code is better.

## Known limits

- `git commit --no-verify` and `git push --no-verify` skip the hooks. So does
  a clone in which nobody ran `cw init`. A check in continuous integration,
  `cw run --base <rev>`, is the backstop.
- A passing run proves that the check passed on that content on that machine.
  It proves nothing about what the check does not test.
- The hooks judge what git is about to record. They do not run the checks:
  `cw run` does, before the commit.
- `cw` reads the records of Claude Code and Codex to count tokens. Their
  format is not documented by either host, so a count can be `unknown`.

More in [evidence](docs/evidence.md) and [hooks](docs/hooks.md).

## The previous workflow

Versions up to 2.0.96 were a larger workflow of skills and task records. It is
kept on the branch `workflow-v3`.

## Documents

[docs/README.md](docs/README.md) lists every document. To try what is here,
start with [docs/try-it.md](docs/try-it.md).

## License

MIT, see [LICENSE](LICENSE). Third-party code: [THIRD_PARTY.md](THIRD_PARTY.md).
