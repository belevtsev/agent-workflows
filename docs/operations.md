# Operations

Claude Code gets the skills and worker agents from the plugin in
`.claude-plugin/`, and Codex from `.codex-plugin/` and
`.agents/plugins/marketplace.json`. This page covers installing them and `cw`,
the Codex instruction block, removing a v2 installation, paths, the model policy
lookup and adviser keys.

## Install

### Claude Code plugin

```sh
claude plugin marketplace add belevtsev/agent-workflows
claude plugin install agent-workflows@agent-workflows
```

Inside a session, `/plugin marketplace add belevtsev/agent-workflows` and
`/plugin install agent-workflows@agent-workflows` do the same. The Go skill pack
is a second plugin in the same marketplace, `cc-skills-golang@agent-workflows`.
The plugin provides the skills and the worker agents
(`agent-workflows:cw-reviewer`, `agent-workflows:cw-test-author`,
`agent-workflows:cw-github-worker`, and the architect, implementer and
test-engineer agents under `agents/`). It does not
edit `~/.claude/CLAUDE.md` or `settings.json`. `cw install --host claude` is
refused with a pointer to the plugin.

### Codex

Use either the Codex plugin or `cw install --host codex`, not both, so each skill
is listed once. The plugin installs the skills only, without the `AGENTS.md`
block:

```sh
codex plugin marketplace add belevtsev/agent-workflows
```

Then run `codex`, open `/plugins`, and install `agent-workflows` (and
`cc-skills-golang` for the Go pack); start a new session afterwards. For the
skills plus the shared instruction block, use
[`cw install --host codex`](#codex-install-and-uninstall) instead. The vendored
Docker skills in `third_party/docker-skills` are in neither plugin; only
`cw install --host codex` links them.

### Get cw

With Go installed, build from the main branch:

```sh
go install github.com/belevtsev/agent-workflows/cmd/cw@main
```

Use `@main` or a commit, not `@latest` or a `v3` tag: the module path has no
major-version suffix, so Go resolves `@latest` to an old v1 release and rejects
`@v3.x.y`. `cw version` then shows a Go pseudo-version that names the commit.

Or download `cw_<tag>_<os>_<arch>.tar.gz` (`darwin` or `linux`, `amd64` or
`arm64`) and `SHA256SUMS` from the
[release page](https://github.com/belevtsev/agent-workflows/releases/latest),
check the archive against `SHA256SUMS`, and put `cw` on your `PATH`. For example,
on an Apple silicon Mac:

```sh
archive=cw_v3.0.0_darwin_arm64.tar.gz
grep " $archive\$" SHA256SUMS | shasum -a 256 -c
tar -xzf "$archive" cw
mkdir -p ~/.local/bin && mv cw ~/.local/bin/cw
```

On Linux, use `sha256sum -c` instead of `shasum -a 256 -c`. Then check the build
with `cw version`, which prints JSON with `version`, `revision`, `built_at`, `os`
and `arch`. Bare `cw` prints help.

## Codex install and uninstall

`cw install --host codex` is an alternative to the Codex plugin for users who also
want the shared instruction block. Use one or the other, not both, so each skill
is listed once.

```sh
git clone https://github.com/belevtsev/agent-workflows "${XDG_DATA_HOME:-$HOME/.local/share}/agent-workflows/src"
cw install --host codex
```

The checkout defaults to `$XDG_DATA_HOME/agent-workflows/src`, else
`~/.local/share/agent-workflows/src`; `--from DIR` selects another one. The
command:

- symlinks every registration in the checkout's `skills-manifest.json` into
  `~/.agents/skills/<name>`;
- writes the block from `global/AGENTS.md` between
  `<!-- agent-workflows-start -->` and `<!-- agent-workflows-end -->` in
  `$CODEX_HOME/AGENTS.md`. Text outside the markers is preserved; text inside is
  replaced on each run;
- prints the recommended settings from `global/codex-defaults.toml` for
  `$CODEX_HOME/config.toml`. It does not edit `config.toml`; copy the settings you
  want yourself.

To update, run `git pull` in the checkout and run `cw install --host codex` again.
It also removes links into the checkout that the manifest no longer registers.

It owns only symlinks in `~/.agents/skills` that point into the checkout. Anything
else at a registered name, such as a directory, a file or a link elsewhere, makes
it fail without changing anything. Remove that entry, or run `cw migrate-v2` when
it is a v2 link.

```sh
cw uninstall --host codex
```

`cw uninstall --host codex [--from DIR]` removes the links into the checkout and
the block, and deletes `AGENTS.md` when nothing else remains in it. Entries that do
not point into the checkout stay. It works after the checkout has been deleted.
Pass the same `--from` you installed with: when no link points into the selected
checkout, it still removes the block and prints a line saying so.

There is no state file, journal, snapshot or rollback. Both commands reject
`--dry-run`. `cw install --host claude` and `cw uninstall --host claude` are usage
errors that point at the Claude Code plugin.

## Moving from v2

v2 kept immutable release snapshots under the state directory and linked the
skills, the Claude Code worker agents and `~/.local/bin/cw` into them. Remove that
installation with the v3 `cw`:

```sh
cw migrate-v2 --dry-run
cw migrate-v2
```

The dry run lists each action without changing anything. `cw migrate-v2` removes:

- the symlinks in `~/.claude/skills/`, `~/.claude/agents/cw-*.md`,
  `~/.agents/skills/` and `~/.local/bin/cw` that point into the v2 state directory;
- while v2's `state.json` or `journal.json` is still in the state directory, the
  managed block in `~/.claude/CLAUDE.md` and `$CODEX_HOME/AGENTS.md`, and in the
  Claude and Codex homes v2 recorded there (the skill links under a recorded
  Claude home go too);
- `current`, `releases`, `bases`, `runtime`, `state.json` and `journal.json` in the
  state directory.

It keeps `workflow-tasks/` and everything else, and running it again is safe:
without the v2 records it never touches `AGENTS.md` or `CLAUDE.md`. If v2 used a
custom state directory, pass it with `--state-dir`. A directory with none of
the entries above has nothing to migrate, and one with release entries but
neither `state.json` nor `journal.json` must be named `agent-workflows`, so a
mistyped path such as `~/.local/state` changes nothing. Relative
paths are resolved against the current directory. If a step fails, the output
lists only the actions completed before it. It
does not touch the model keys v2 wrote into `~/.claude/settings.json` or
`$CODEX_HOME/config.toml`, or the `PATH` line v2 added to a shell profile; edit
those by hand if you want them gone.

Order:

1. Install the v3 `cw` ([get cw](#get-cw)) and run `cw migrate-v2`.
2. Install the plugins.
3. For Codex, if you want the instruction block, run `cw install --host codex`
   instead of installing the Codex plugin.

Run `cw migrate-v2` before `cw install --host codex`: while the v2 records exist,
it strips the same markers from `AGENTS.md`.

## Paths

Global flags apply to every command:

| Root | Resolution |
| --- | --- |
| Home | `--home`, else the user's home directory |
| Codex home | `--codex-home`, else `$CODEX_HOME`, else `~/.codex` |
| Claude Code home | `--claude-home`, else `$CLAUDE_CONFIG_DIR`, else `~/.claude` |
| State | `--state-dir`, else `$XDG_STATE_HOME/agent-workflows`, else `~/.local/state/agent-workflows` |
| Configuration | `--config-dir`, else `$XDG_CONFIG_HOME/agent-workflows`, else `~/.config/agent-workflows` |

The task store is `<state>/workflow-tasks`. The configuration directory holds the
saved adviser keys (`credentials.json`) and `preferences.json`. `--dry-run` is a
global flag; `cw install`, `cw uninstall` and `cw keys set|unset` reject it.

## Model policy

`cw consult` and `cw task start` select the model policy in this order:

1. `--policy FILE`;
2. `$CW_MODEL_POLICY`;
3. `~/.agents/skills/task-orchestration/model-policy.yaml`, when anything exists at
   that path (a path that exists but cannot be read is reported, not skipped);
4. the policy embedded in `cw`.

The runtime reads only `version`, `consultation.deadline_seconds` and
`consultation.providers` from it. The worker routing in
[model-policy.yaml](../skills/task-orchestration/model-policy.yaml) and
[model-policy.claude.yaml](../skills/task-orchestration/model-policy.claude.yaml)
is read by the coordinator through the skill, not by `cw`.

## Global project diagnostics and evidence

`cw doctor --repo /absolute/project/path` selects the project directly, without
requiring a clean project. It reports the target worktree/content identity,
discovered capabilities, repository contract paths, optional tool executables and
adviser key presence. `--live` explicitly enables bounded local integration checks;
ordinary metadata does not prove semantic retrieval, dirty-source freshness or
runtime call targets. Missing or unsupported tooling is qualified and retains normal
source discovery. Neither mode installs dependencies, changes watcher enrollment or
calls advisers. `--summary` prints one line with only the checks that are missing,
degraded or stale.

`cw task` keeps source-bound requirements, actual argv check execution, references,
the adviser binding fixed at `cw task start`, two substantial-task decision
checkpoints (unless the plan declares them not required, with a reason that
verify lists) and the coordinator's decisions in a private
external task directory, by default `STATE/workflow-tasks`. Its `start`, `assign`,
`handoff`, `accept`, `assess`, `consult`, `run` and `verify` commands accept `--summary` for a
one-line result instead of the whole record (`run`: the whole check run, plus failing
test names and first error lines when it failed; `verify`: without its fixed
limitations), `cw doctor` and `cw consult` accept it too, `cw task show
--field <dotted.path>` prints only selected values, `cw task bundle` writes the
reviewer bundle a review assignment must carry (one reach row per changed file),
`cw task contract-extract` prints requested Go declarations without bodies for a
test author's contract, and
`handoff`, `accept` and `assess` fill omitted fields that have exactly one valid
value (task, assignment hash, agent, current snapshot, result hash, coordinator
and file hashes) while validating explicit values as before. `cw metrics schema` describes the versioned observation format;
`cw metrics summarize` reads supplied observations without changing project source.
`cw metrics import` reports per-agent tokens and dollars from a Claude Code transcript
or a Codex rollout, without writing anything; with `--composition` and `--prices` it also
attributes each agent's input to its sources (Claude transcripts only).
Unknown metrics remain unknown, and synthetic observations are never described as
production savings. See the [global command guide](../skills/task-orchestration/references/global-workflow.md).

The shared instructions apply across projects. A first-party Go repository, a
Terraform repository and a Roblox project retain their own checks and operational
rules. Native task commands execute only explicitly selected argv; repository text,
measurement input and model advice do not authorize publication or deployment.

## Consultation command

`cw consult` calls the configured advisers: the task's bound set for a task-bound
request, otherwise the providers enabled in the [selected policy](#model-policy)
and not disabled in your preferences, each only with its key present. It works
from any directory. With no adviser configured it writes a
`no_advisers_configured` record without network access:

```sh
cw consult --request /path/to/request.json --output /path/to/new-record.json --dry-run
```

See the [helper contract](../skills/task-orchestration/references/consultation-helper.md)
for request and record formats.

## Optional Graphify runtime

The first-party [Graphify skill](../skills/graphify/SKILL.md) uses the external
`graphifyy==0.9.103` package. Provision this optional runtime explicitly when
activating Graphify on a device. `cw graphify` supports macOS and Linux. That
package is a Python tool, so this optional runtime is the one place the workflow
touches Python, and it lives outside this repository. Python 3.14 with `venv` and
`ensurepip` is the recommended interpreter for this pin:

```sh
python3.14 -m venv "$HOME/.venvs/agent-graphify"
"$HOME/.venvs/agent-graphify/bin/python" -m pip install 'graphifyy==0.9.103'
"$HOME/.venvs/agent-graphify/bin/python" -m pip check
```

Use an explicit path to a suitable Python interpreter if `python3.14` is not on
PATH, and leave the system Python defaults unchanged. Installing the plugins,
`cw install --host codex` and `cw doctor` do not install Graphify, invoke its CLI,
or create graph caches. `cw doctor` reports a detected `graphify` executable as
`graphify_cli`; detection does not prove version compatibility or a complete graph.

Use the guarded command:

```sh
cw graphify build --repo /path/to/repo
cw graphify query --repo /path/to/repo 'Which packages and symbols relate to the consultation record?' --budget 2000
```

It finds `~/.venvs/agent-graphify/bin/graphify`, then `graphify` on PATH; `--runtime`
names another executable. It snapshots eligible tracked Go source and module
metadata, omitting vendored and fixture trees. It fixes local code-only flags,
disables LLM deduplication, sets `GRAPHIFY_NO_AUTO_REFRESH=1`,
`GRAPHIFY_QUERY_LOG_DISABLE=1` and `GRAPHIFY_BACKEND=none`, and launches the CLI
with a minimal environment. These values are fixed; the caller's own `GRAPHIFY_*`
settings are not forwarded. Queries pass `--depth 2` explicitly, so a repository
`.graphifyrc` cannot change their traversal.
Each CLI child also receives temporary `HOME` and XDG configuration/cache
directories, keeping the user's global provider configuration out of that child.
It adds no hooks, watch processes, cloud integration, MCP configuration, or memory
writes.

The cache is outside both the repository and the installed skills, keyed by
worktree, HEAD, tracked source content, extraction scope, and tool identity.
Cache child components and entries that redirect through symlinks are rejected.
Queries refuse missing, stale, or incomplete caches; build again after relevant
tracked source or runtime changes. Graph relationships, including `INFERRED`
ones, are structural hints. Verify source and resolved Go symbols before claiming
runtime callers, interface implementations, or dependency-injection targets.
Missing runtime or graph coverage leaves direct reads, `rg`, semantic search,
and Go tooling available.

## Adviser keys

The shipped policy enables `subq_snap` and `openai_decisions`; `jev` is opt-in. An
enabled adviser participates only with its key (`SUBQ_API_KEY`, `OPENAI_API_KEY` or
`TYPESAFE_API_KEY`) and when it is not disabled in your preferences.

```sh
cw keys set SUBQ_API_KEY < /path/to/private-key-file
cw keys set OPENAI_API_KEY --from-env OPENAI_API_KEY
cw keys status
cw keys unset SUBQ_API_KEY
```

`cw keys set NAME` reads the key from standard input, minus one trailing line
break, or with `--from-env VAR` from that environment variable; it never takes the
key from the command line. Saved keys live in a plaintext `credentials.json` in the
configuration directory; the directory must have mode `0700` and the file `0600`,
both owned by you and not linked. A key in the environment takes precedence over a
saved one. `cw keys status` reports whether each key comes from the environment,
the saved file or neither, without any value or provider call.

To disable an adviser and keep its key, list its provider ID under
`advisers.disabled` in `preferences.json` in the configuration directory, for
example `{"version":1,"advisers":{"disabled":["openai_decisions"]}}`. Edit the file
by hand; it must be a regular file with mode `0600`, owned by you. An unreadable or
invalid file makes `cw task start` fail before it creates a task. The change applies
to tasks started afterwards: `cw task start` binds the advisers configured at that
moment. A bound adviser whose key is missing at consultation time is recorded as
`not_configured`; `cw consult` still writes the record, exits `2` and names the
variable, and `cw task consult` rejects that record. Supply the key and consult
again. Installation and local validation do not require keys or make hosted
requests.

A terminal export does not update an already running desktop app; use the
[setup instructions](advisers.md#make-keys-visible-to-the-agent) and a fresh
process and session. `cw consult` never reads macOS launchctl or another process's
secrets by itself.

See the [consultation record contract](../skills/task-orchestration/references/consultation-helper.md)
for adviser statuses, offline adjudication and exit codes.
