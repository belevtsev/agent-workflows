# Installation operations

## Install and inspect

Run the downloaded binary with `cw install` (or bare `cw`). A normal installation
uses its embedded resources and never clones or builds a repository. `cw update`
acquires one pinned stable release before handing off to its verified executable.

In an interactive terminal the four-step setup chooses hosts, model preferences,
optional private API keys, and a before/after review on the alternate screen.
← or Backspace returns to the previous step with earlier answers kept; Esc asks
before quitting and Ctrl-C quits at once. Quitting before apply prints
"Setup cancelled", exits with status 130 and leaves active configuration
unchanged. A review taller than the terminal scrolls with PgUp/PgDn. After a
successful interactive apply, setup prints a short summary to stderr and no JSON;
run `cw status` for the full report. `--plain` and `TERM=dumb`/`NO_COLOR` use the
line-based interface. Machine modes (`--json`, piped streams, `--non-interactive`)
and dry runs do not prompt and write the JSON report to stdout.

```sh
cw install --host all --preset recommended --non-interactive
cw update
cw status
```

Updates keep saved preferences by default. Recommended and Balanced replace saved
overrides, including memory/context values, as explained before review. They keep
the GitHub worker choice: the models step shows one GitHub workers row per selected
host, and selecting it sets one model and effort for `commit_execution`,
`pr_authoring_routine` and `pr_submission`. Release default unpins all three. When
those roles hold different values, the row shows "differs per role"; a preset
keeps them, and picking a value replaces all three. A saved GitHub value that a
newer release no longer offers gives way to the preset. Advanced
lets each role and fallback inherit release values or pin model/effort fields
independently. The Advanced menu edits individual fields; Enter keeps the shown
value, while `default` clears a text override. `--settings FILE` accepts nonsecret preferences instead of a preset:

```json
{"version":1,"hosts":{"codex":{"roles":{"coordinator":{"effort":"max"}}}},"shared":{"memories.use_memories":true}}
```

Missing fields inherit; explicit `null`, unsupported hosts/roles/models/efforts or
incompatible scope are refused. All host and native policy readers use the same
immutable effective snapshot, whose digest differs from the pristine base digest.

Private preferences and credentials use `$XDG_CONFIG_HOME/agent-workflows` or
`~/.config/agent-workflows`, unless initial `--config-dir` selects an absolute root.
The sealed installation records that location. Changing XDG does not move it, and
a conflicting later explicit root is refused. Uninstall retains user preferences
and keys. Stored keys require directory0700 and file0600, current-user ownership,
and regular nonlinked files. No key values appear in status, journals or JSON.
Environment keys take precedence; a saved replacement remains shadowed while its
environment key exists. Optional key checking sends only a tiny synthetic request,
one attempt per available provider under a common30-second budget, and is off by
default. Missing keys do not prevent installation; an adviser without its key, or
disabled in your preferences, does not participate. Adviser failure remains missing coverage; successful key checking
does not prove model availability in a host.

If settings commit but credential saving or checking fails or is cancelled, the
command reports a nonzero partial outcome. A post-rename storage-sync failure is
reported as committed with persistence unconfirmed; reload the file before retrying.

## Interrupted setup

Status, rollback, recovery and uninstall work from installed records. Pending
journals prevent setup prompts and new mutations; run `cw recover` explicitly.
Installations use state/journal4 and manager v10. Cached but unactivated
candidates do not establish installed version authority. Re-running install/setup
uses the active cached base and retains the manager and activation history;
explicit preference edits derive a new effective snapshot of that same base.
Updates select a newer base. Runtime locator host paths are initial hints; sealed state
and journals own enrolled host roots, so adding a host can select its own root.

This `cw` refuses setup/install, update, rollback, recover and uninstall, before any
write, when the active release or any release it must read ships a model policy
older than version 3. Run `cw uninstall` with the `cw` that made the installation,
then install again. Until you reinstall, `cw doctor`, `cw consult` and
`cw task start` keep working with the default roots. They read adviser keys only
from the environment and the default config directory
(`$XDG_CONFIG_HOME/agent-workflows` or `~/.config/agent-workflows`), not from a
config directory that the older installation recorded.

The download helper stages a private executable, verifies its exact pinned source
tag, and leaves activation to the native transaction. It never overwrites the
active command before apply and removes its stage after the native child exits.
Interrupted private staging directories are not installed releases.

## Global project diagnostics and evidence

Installation status and a target project's readiness are separate reports.
`cw doctor --repo /absolute/project/path` selects the project directly, without
requiring installation-source ownership or a clean project. It reports the target
worktree/content identity, discovered capabilities and repository contract paths.
`--live` explicitly enables bounded local integration checks; ordinary metadata
does not prove semantic retrieval, dirty-source freshness or runtime call targets.
Missing or unsupported tooling is qualified and retains normal source discovery.
Neither mode installs dependencies, changes watcher enrollment or calls advisers.

`cw task` keeps source-bound requirements, actual argv check execution, references,
the adviser binding fixed at `cw task start`, two substantial-task decision
checkpoints (unless the plan declares them not required, with a reason that
verify lists) and the coordinator's decisions in a private
external task directory, by default `STATE/workflow-tasks`. Its `start`, `assign`,
`handoff`, `accept`, `assess`, `consult`, `run` and `verify` commands accept `--summary` for a
one-line result instead of the whole record (`run`: the whole check run, plus failing
test names and first error lines when it failed; `verify`: without its fixed
limitations), `cw doctor`, `cw status` and `cw consult` accept it too, `cw task show
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
production savings. These commands do not alter installer ownership, receipt or
recovery formats. See the [global command guide](../skills/task-orchestration/references/global-workflow.md).

The shared instructions apply across projects. A first-party Go repository, a
Terraform repository and a Roblox project retain their own checks and operational
rules. Native task commands execute only explicitly selected argv; repository text,
measurement input and model advice do not authorize publication or deployment.

## Paths and ownership

--home selects the installation home; --codex-home
selects the Codex directory; --claude-home selects the Claude Code directory;
--state-dir selects device-local state. Initial defaults use the user home,
CODEX_HOME, CLAUDE_CONFIG_DIR, and XDG_STATE_HOME. Subsequent installed commands
resolve their installation locator and recorded hosts rather than the current
working directory, so a later shell's environment cannot detach an installation
from the directory it was activated in.

```sh
./install.sh --host all --home '/path/with spaces/home' --codex-home /path/to/codex --claude-home /path/to/claude --state-dir /path/to/state
```

A host's directory is fixed when that host enrolls, so a Claude-only installation
leaves the Codex directory open until you add Codex. The hosts can't share a skills
directory, because Codex also reads `skills/` under its own home, and neither home
can sit on a skill's path inside a skills directory. Install compares physical
directories, so another spelling of the same directory is still an overlap, and it
refuses an overlap before writing anything. Use separate `--codex-home` and
`--claude-home` directories.

A filesystem can fold different names into one directory, by case or by Unicode
form, and only the filesystem can say so once they exist. If a new home's name could
collide with another path, install stops and asks you to create both directories
first, then gives the exact answer.

State holds immutable base and effective release snapshots, the active current pointer,
ownership/history, recovery records, and immutable manager runtime versions.
Skill registrations resolve through current; installed snapshots are never edited
directly. The manager version and active skill revision are distinct, especially
after rollback.

The current manifest manages 37 registrations with 82 entrypoints on each host, a
marked block in that host's global instructions, its model defaults, and one
enrolled command/PATH registration. It preserves unrelated content, comments,
modes, credentials, and repository instructions. Original settings and adopted
registrations remain reserved for uninstall. Modified owned content is a conflict,
except for the Claude Code settings keys described below.

State records each host separately under `hosts`. One release, `current` pointer
and manager serve every host, so an update or rollback activates them together.

## Claude Code host

Links live in `<claude-home>/skills/<name>`. `cc-skills-golang` keeps its single
link: Claude Code loads that folder as the `cc-skills-golang@skills-dir` plugin
because it carries `.claude-plugin/plugin.json`, so its skills appear as
`cc-skills-golang:<skill>`. A name already present in `skills/` is a conflict.
Personal skill adoption applies only to Codex.

Agent definitions link `<claude-home>/agents/<name>.md` to the files the release
manifest names under `claude_agents` (`global/claude-agents/`), through the
`current` pointer like skill links. They are `cw-reviewer`, `cw-test-author` and
`cw-github-worker`, each with `disallowedTools: Skill` and no model or effort, so
the coordinator's dispatch sets both. The released files are read-only, so a write
through the link, for example from Claude Code's agent editor, fails. Like a skill
file, a definition replaced inside the release makes the next command report the
snapshot as modified. An existing file of the same name is a conflict; other files
in `agents/` are left alone. When the release names definitions or cw owns some,
`agents/` must be a real directory, not a symlink, as `skills/` must; move a
symlinked directory's files into a real one before updating. Update and rollback
add and remove links to match the target release, and uninstall removes them. A
changed or deleted owned link stops the next command; restore it with
`ln -sfn STATE/current/global/claude-agents/NAME.md <claude-home>/agents/NAME.md`.
Codex hosts get none.

The instructions block is rendered from the same template as Codex, with the same
markers, into `<claude-home>/CLAUDE.md`.

`settings.json` gets exactly two keys from the coordinator profile in
`model-policy.claude.yaml`: `model` and `effortLevel` (`claude-opus-5-5` and
`high` by default; `xhigh` remains for the decision roles). The edit changes those
values' bytes and nothing else. Malformed JSON, a non-object document, or a
symlinked file stops installation before anything changes.

Claude Code can rewrite this file itself, for example when you change model in the
app, so these keys don't follow the conflict rule. A key that no longer holds the
managed value is yours:

- `cw status` lists it under `hosts.claude.settings_drift`.
- Update and rollback leave it alone and don't refuse.
- Uninstall restores only keys that still hold the managed value.
- An interrupted transaction skips a key that moved to a third value.

## Hosts

Add a host at any time. It joins the active release with no new history entry:

```sh
./install.sh --host codex
```

An addition reads the installed release and leaves its base, activation history
and manager alone, including after rollback. Ordinary standalone install/setup
also reconfigures this cached base when all selected hosts are already enrolled.
Explicit preference edits can change its effective digest; `cw update` selects a
new base.

Remove one host and keep the rest. This restores that host's files and leaves the
shared release, manager and other host alone:

```sh
cw uninstall --host codex --dry-run
cw uninstall --host codex
```

If that host's directory is already gone, `cw uninstall --host codex --forget`
removes its record without touching files. Without `--forget`, missing owned files
are reported as changes, and every other command keeps refusing. Removing the last
host is the same as a full `cw uninstall`.

## Shared Codex configuration

The current optional version-one `global/codex-defaults.toml` resource delivers
exactly these six portable values through install and update:

```toml
model_context_window = 512000

[features]
memories = true

[memories]
generate_memories = true
use_memories = true
extract_model = "gpt-6.1-sol"
consolidation_model = "gpt-6.1-sol"
```

Its `version = 1` belongs to the source resource and is not written into the
device's config. Historical version-one resources contain exactly the same five
memory keys and omit `model_context_window`; both complete groups are supported.
Additional settings, other missing keys, mixed ownership groups, or incorrect
value types are rejected. Historical snapshots without the resource remain valid.
`model_context_window` must be a positive integer number of tokens. Codex applies
it to whichever model that config runs, so keep it within every model's maximum;
the current catalog allows 872,000 for each listed model.
`cw status` reports shared ownership and the active snapshot's defaults in
`shared_config`.

The manager merges only owned value tokens into the selected Codex home's
`config.toml`. Device-specific paths, MCP configuration, project trust,
credentials, unrelated memory settings, comments, and current file permissions
are preserved. It records each shared key's value or absence when first acquired,
separately from the existing root-model ownership. Updating expected defaults
does not replace those saved originals for retained keys. Moving from five to six
keys acquires the context window using its current personal value or absence,
including a value added after the original five-key enrollment. Moving back to
five keys restores that saved token and releases context-window ownership. Later
personal edits to the released key are preserved; reacquiring it saves its latest
value. A snapshot without the resource restores and releases the entire group.

Explicit `[features]` and `[memories]` tables and dotted keys are supported.
Inline parent tables such as `features = { memories = false }` or
`memories = { extract_model = 'local' }` are unsupported and cause a preflight
error before installation changes. Convert those parents to explicit tables or
dotted keys while retaining their other local values, then retry. Malformed TOML,
wrong owned value types, and a symlinked config also stop installation.

All involved root and shared values, including keys being released, use one
recoverable config-file operation. Changes to an already owned value cause a
conflict; a newly acquired key is saved as personal origin. Independent local edits
made before rendering or during interrupted recovery survive the value merge.

## Consultation command

`cw consult` calls the configured advisers: the task's bound set for a task-bound
request, otherwise the providers enabled in the active release's policy and not
disabled in your preferences, each only with its key present. It reads the active
release's policy and question files, so it works from any directory. With no
adviser configured it writes a `no_advisers_configured` record without network
access:

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
PATH, and leave the system Python defaults unchanged. Generic install, update,
validation, and status do not install Graphify, invoke its CLI, or create graph
caches.
`cw status` reports optional prerequisites offline; detected executables do not
prove version compatibility or a complete graph.

Use the guarded command:

```sh
cw graphify build --repo /path/to/repo
cw graphify query --repo /path/to/repo 'Which packages and symbols relate to the installation transaction?' --budget 2000
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

The cache is outside both the repository and immutable skill release, keyed by
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
`TYPESAFE_API_KEY`) and when it is not disabled in your preferences. To disable one
and keep its key, clear its "Use at checkpoints" box in setup's Keys step, or list
its provider ID under `advisers.disabled` in a `--settings` file; the change applies
to tasks started afterwards. `cw task start` binds the advisers configured at that
moment.
A bound adviser whose key is missing at consultation time is recorded as
`not_configured`; `cw consult` still writes the record, exits `2` and names the
variable, and `cw task consult` rejects that record. Supply the key and consult
again. Installation, local validation and status
do not require keys or make hosted requests.

`cw status` reports in `credential_sources` whether each key comes from its own
environment or the private stored file, without calling a provider. A terminal
export does not update an already running desktop app; use the
[setup instructions](../README.md#make-keys-visible-to-the-agent) and a fresh
process and session. `cw consult` never reads macOS launchctl or another process's
secrets by itself.

See the [consultation record contract](../skills/task-orchestration/references/consultation-helper.md)
for adviser statuses, offline adjudication and exit codes.

## Direct cw command

The managed ~/.local/bin/cw link executes the Go binary beneath the selected
state directory. It never calls install.sh. A PATH block is added only when
necessary. Use --shell bash, --shell zsh, or --shell none during install to
override shell selection. Unknown shells and custom zsh startup locations are
reported for manual PATH enrollment. Existing unowned cw definitions/paths are
not replaced.

Open a new terminal after installing so the shell picks up the PATH entry. Retain
unrelated shell/profile edits.

## Moving from an older install

This `cw` reads only installations made by the standalone manager, first released
in v2.0.58. When it finds one made by an earlier `cw` (a source-checkout manager or
codex-workflows v1), or one that v2.0.58 migrated from such an install, every
lifecycle command refuses before writing anything
and asks you to run `cw uninstall` with your existing `cw`. Read-only commands
such as `cw doctor`, `cw consult` and `cw task` ignore such an installation and
use the default roots.

Move once:

1. Run `cw uninstall` with the existing `cw`.
2. Run the download installer with `--host all`, adding `--adopt-personal-skills`
   if you had adopted personal skills.

Old task records and rollback history do not carry over. An older task record is
refused; start a new task with `cw task start`.

## Adopting standalone personal skills

Only install/setup accepts --adopt-personal-skills, and it applies to the Codex host.
A fresh installation with absent destinations needs no flag. An existing standalone
production-plan, Archify, or Docker skill requires explicit adoption:

```sh
./install.sh --adopt-personal-skills --dry-run
./install.sh --adopt-personal-skills
cw status
```

The manifest refers to personal-skill-origins.json. Its source-relative hashes,
paths, and modes describe the verified originals, including files deliberately
omitted from the distributed Archify runtime. Exactly one matching directory
may exist per name across ~/.agents/skills, the selected Codex skills directory,
and standard ~/.codex/skills. Duplicate directories, modified contents or modes,
dangling links, symlink aliases, unsafe paths, occupied backups, and a backup on
an incompatible filesystem are conflicts. No copy fallback is attempted.

The manager moves originals into STATE/backups/personal-skills/NAME and records
their inventories. Adoption composes the original directory, backup, and managed
link into one recoverable operation; this also handles Archify occupying its own
managed registration path. All mutation paths are checked for overlap before
activation and recovery. Uninstall checks the owned links and original backup
inventories before restoring the directories. Changed owned content or backups
stop the transaction. No credentials or personal tool configuration are adopted.

Rollback refuses a target snapshot that omits an adopted registration. Keep the
current release or uninstall to restore originals; do not remove adoption records
to bypass this protection. Ordinary absent-origin additions remain removable by
rollback.

Forward install/update may add new registrations but refuses removal, rename, or
arbitrary retargeting of existing ones. New names are not adopted from occupied
paths, including dangling symlinks, during ordinary installation or update.

## Update and rollback

Normal standalone setup uses the verified active cached base on an existing
state4 installation. Repeating unchanged choices is a no-op. Update is manual:

```sh
cw update --dry-run
cw update
cw status
```

A preview checks local evidence and defers release discovery without network or
persistent writes. Applied update pins one stable release, verifies
all platform/checksum metadata and its native archive, then hands off to that
verified binary with the captured roots, expected state generation and target
identity. The target rechecks ownership before activation. Concurrent changes,
older versions and conflicting same-version identities refuse; cached candidates
never raise installed version authority. Update never clones, fetches or builds a
repository.

```sh
cw rollback --dry-run
cw rollback
```

Rollback restores the previous skills and owned instruction/model defaults, while
keeping a compatible manager. It does not change Git or GitHub. It keeps saved
preferences and refuses when they pin a role the previous release lacks, such as
the Claude `review_followup` role; clear that pin in setup first (Advanced, then
`Release default` for that role's model and effort).
Status then reports the restored model profiles. Hosts are not part of a release,
so rollback never adds or removes one, and a host you removed stays removed.
Shared settings follow the previous snapshot's resource. A six-to-five-key
rollback restores and releases the context window while preserving the five
memory keys' saved originals. Returning to a snapshot without that resource
restores the original shared values or their absence and releases shared
ownership. The earliest config-file origin remains recorded while any config
ownership remains, including when shared ownership was acquired before root-model
enrollment or a later personal context window was acquired.
The target snapshot must have a subset of the active registration names. A path
absent from that snapshot is removed only if its recorded origin was absent and
its current link remains exactly owned. Adopted symlink/directory origins cannot
be dropped; their restoration remains an uninstall responsibility.
Install/update/rollback require a fresh agent session to observe new instructions.

## Interrupted activation and removal

```sh
cw recover --dry-run
cw recover
```

Recovery reverses journaled owned changes, preserving unrelated edits and refusing
changed owned content. It includes runtime/command changes, and is a no-op without
a pending transaction. It does not delete caches.
Use a verified cached executable or ./install.sh recover if a crash occurred
before cw enrollment. Do not hand-edit ownership or recovery records.

If recovery is itself interrupted and `cw` no longer starts a v10 manager, read
only `recovery_runtime_key` from `STATE/journal.json`, without editing the
journal, and use that exact preserved runtime. The key includes source
revision and base digest; it is not just the revision. Replace the placeholders:

```sh
STATE/runtime/releases/RECOVERY_RUNTIME_KEY/cw --manager-protocol
STATE/runtime/releases/RECOVERY_RUNTIME_KEY/cw recover --dry-run
STATE/runtime/releases/RECOVERY_RUNTIME_KEY/cw recover
```

Verify `cw-manager-v10` before recovering a version4 journal. The cached executable
uses its sealed locator and journal roots.

A journal can name paths that an older `cw` of the same protocol does not know,
such as Claude agent links, and that `cw` then refuses it as unsafe. If `cw recover`
fails that way after an interrupted update, use the runtime named by
`recovery_runtime_key` as above.

The download-only `install.sh recover` is another option. It can run from any
directory; pass the explicit recorded roots if the installed command is
undiscoverable.
Skill rollback retains the compatible manager.

```sh
cw uninstall --dry-run
cw uninstall
```

Uninstall restores adopted registrations/original model and shared memory values
and removes owned
registrations, global/PATH blocks, and the command link. It preserves immutable
caches, unrelated settings, credentials, and connectors. Reinstall through
./install.sh. Status, recovery, rollback, and uninstall use only installed records.
It preserves later unrelated config content and current permissions. If the file
was absent before the first config ownership was acquired, removal deletes it
only when restoration leaves zero bytes; later comments, whitespace, or settings
keep it in place.
