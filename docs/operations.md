# Installation operations

## Install and inspect

Use clean committed source, and name the host you are installing for:

```sh
./install.sh --host claude --dry-run
./install.sh --host claude
cw status
```

`--host` accepts `auto`, `codex`, `claude` or `all`. `auto` uses the hosts already
recorded. A fresh installation uses the one host it detects from `CODEX_HOME` or
`~/.codex` and `CLAUDE_CONFIG_DIR` or `~/.claude`, and stops if it finds both or
neither, rather than writing to a second tool's configuration unasked.

The bootstrap obtains a verified executable, then Go performs the installation.
The default action is install; setup remains an alias. Mutations apply by default;
add --dry-run for a read-only local preview. A cold bootstrap preview reports
deferred validation without downloads or writes. cw status itself is offline.
The manager and its helpers run in Go, so installation needs no Python. Skills can
bring their own dependencies; `cw status` reports detected tools.

Before a legacy v7/v8 installation activates this source with `cw update`, follow
the [v9 rebootstrap procedure](development.md#distribution) using the new
installer. Its required latest release must already provide verified
`cw-manager-v9` binaries and checksums for all supported platforms.

Status reports `model_profiles` with each configured model, reasoning effort, and
scope from the active snapshot, and `hosts` with each host's roots. This is policy evidence. It does not identify the
model or effort used by an existing chat or establish account runtime availability.

## Interrupted bootstrap

The bootstrap serializes cache acquisition with `.bin.lock` in its checkout.
It records the owning process ID in `.bin.lock/pid` and the UTC acquisition time
in `.bin.lock/started`. Normal exit removes its own lock. SIGKILL or a reboot can
leave it behind; another invocation reports the owner and this recovery procedure.
Locks are never reclaimed automatically because a PID alone cannot distinguish a
live owner from PID reuse, and missing metadata does not prove that an owner died.

From the affected checkout, inspect the recorded PID and time, then inspect that
process. Replace `PID` below with the recorded value:

```sh
cat .bin.lock/pid .bin.lock/started
TZ=UTC ps -p PID -o pid=,lstart=,command=
```

Wait for a live bootstrap and its download children to finish. For an abandoned
lock, establish that its owner and children have exited and stop other bootstrap
or recovery attempts for this checkout while removing the lock. Older bootstraps,
or interruption before metadata was written, can leave an empty lock: in that case
establish that no bootstrap is running for this checkout before proceeding.

Remove only the two known metadata files and the empty, real lock directory:

```sh
(
    set -eu
    test -d .bin.lock
    test ! -L .bin.lock
    rm -f -- .bin.lock/pid .bin.lock/started
    rmdir .bin.lock
)
```

If the directory is not empty, inspect its contents instead of deleting it
recursively. Retry the original `./install.sh` command. This procedure leaves
cached managers and installation state untouched. Abandoned `.bin/.bootstrap.*`
download directories are not activated and do not prevent the retry.

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
two substantial-task adviser checkpoints and the coordinator's decision in a private
external task directory, by default `STATE/workflow-tasks`. `cw metrics schema` describes the versioned observation format;
`cw metrics summarize` reads supplied observations without changing project source.
Unknown metrics remain unknown, and synthetic observations are never described as
production savings. These commands do not alter installer ownership, receipt or
recovery formats. See the [global command guide](../skills/task-orchestration/references/global-workflow.md).

The shared instructions apply across projects. A first-party Go repository, a
Terraform repository and a Roblox project retain their own checks and operational
rules. Native task commands execute only explicitly selected argv; repository text,
measurement input and model advice do not authorize publication or deployment.

## Paths and ownership

--source selects the checkout. --home selects the installation home; --codex-home
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

State holds immutable releases/<SHA> snapshots, the active current pointer,
ownership/history, recovery records, and immutable manager runtime versions.
Skill registrations resolve through current. The source checkout remains editable;
installed snapshots are never edited directly. The manager version and active
skill revision are distinct, especially after rollback or --no-checkout.

The current manifest manages 33 registrations with 78 entrypoints on each host, a
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

The instructions block is rendered from the same template as Codex, with the same
markers, into `<claude-home>/CLAUDE.md`.

`settings.json` gets exactly two keys from the coordinator profile in
`model-policy.claude.yaml`: `model` and `effortLevel`. The edit changes those
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

An addition reads the installed release, not the checkout, and leaves the release,
history and manager alone. It never undoes a rollback, and a dirty or older checkout
doesn't block it. Naming only hosts that are already recorded isn't an addition, so
that install activates HEAD as usual.

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
extract_model = "gpt-6-astra"
consolidation_model = "gpt-6-astra"
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

`cw consult` runs the three adviser calls. It reads the active release's policy and
question files, so it works from any directory and needs nothing but the provider
keys in its environment:

```sh
cw consult --request /path/to/request.json --output /path/to/new-record.json --dry-run
```

See the [helper contract](../skills/task-orchestration/references/consultation-helper.md)
for request and record formats.

## Optional Graphify runtime

The first-party [Graphify skill](../skills/graphify/SKILL.md) uses the external
`graphifyy==0.9.91` package. Provision this optional runtime explicitly when
activating Graphify on a device. `cw graphify` supports macOS and Linux. That
package is a Python tool, so this optional runtime is the one place the workflow
touches Python, and it lives outside this repository. Python 3.14 with `venv` and
`ensurepip` is the recommended interpreter for this pin:

```sh
python3.14 -m venv "$HOME/.venvs/agent-graphify"
"$HOME/.venvs/agent-graphify/bin/python" -m pip install 'graphifyy==0.9.91'
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
disables LLM deduplication, sets `GRAPHIFY_NO_AUTO_REFRESH=1` and
`GRAPHIFY_QUERY_LOG_DISABLE=1`, and launches the CLI with a minimal environment.
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

## Required OpenAI adviser

The current policy declares `openai_decisions: gpt-6-luna` under
`consultation.providers`, alongside Jev and SubQ Snap. Supply `OPENAI_API_KEY`
together with `TYPESAFE_API_KEY` and `SUBQ_API_KEY`. Every consultation includes all
three required advisers; a missing key records unavailable coverage and preserves
the other results. Excluding OpenAI is rejected for this policy. Installation,
local validation and status do not require credentials or make hosted requests.

`cw status` reports `openai_credential_present` from its own environment without
calling OpenAI. A terminal export does not update an already running desktop app;
use the [setup instructions](../README.md#set-up-openai-decisions) and a fresh
process and session. `cw consult` never reads macOS launchctl or another process's
secrets by itself.

See the [consultation record contract](../skills/task-orchestration/references/consultation-helper.md)
for unavailable/refused coverage and offline adjudication.

## Direct cw command

The managed ~/.local/bin/cw link executes the Go binary beneath the selected
state directory. It never calls install.sh. A PATH block is added only when
necessary. Use --shell bash, --shell zsh, or --shell none during install to
override shell selection. Unknown shells and custom zsh startup locations are
reported for manual PATH enrollment. Existing unowned cw definitions/paths are
not replaced.

Open a new terminal after installing so the shell picks up the PATH entry. Retain
unrelated shell/profile edits.

## Upgrading from v1

The v2 manager doesn't read a v1 installation. Its manifest version makes a v1
`cw update` refuse this source before anything changes, so moving across is a
one-time reinstall:

1. Note `state_dir` from `cw status`. If `cw status` reports a modified release
   because of a stray `__pycache__`, delete that directory inside the release first.
2. Run `cw uninstall` with the existing `cw`.
3. From a clean checkout of this repository, run
   `./install.sh --host all --adopt-personal-skills`.
4. Verify with `cw status`, then delete the old `state_dir` and the old adviser
   Python environment.

Rollback history from v1 doesn't carry over. Records under `$CODEX_HOME/workflow-tasks`
stay in place and remain readable with `--store`.

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

Install validates local HEAD without fetching. After acquisition, repeated update
or setup is a no-op when owned content and runtime match. A clean local
fast-forward can be installed;
divergent or rewound activation is refused. Updates are always manual:

```sh
cw update --dry-run
cw update
cw status
```

A preview checks local evidence without contacting GitHub. Applied update fetches
origin/main, validates the exact commit, prepares a verified manager candidate,
fast-forwards clean source, and activates owned changes recoverably. Candidate
preparation failure leaves the active installation unchanged. --no-checkout keeps
source HEAD while installing the fetched snapshot. Git uses existing authentication.
New destinations are preflighted before the Git fast-forward and checked again
before activation. Existing conflicts leave source HEAD and activation unchanged;
a subsequent external edit can still stop activation after Git advances. Recovery
preserves that fast-forward. Dry-run registration changes describe local evidence
without fetching a remote candidate.

```sh
cw rollback --dry-run
cw rollback
```

Rollback restores the previous skills and owned instruction/model defaults, while
keeping a compatible manager. It does not rewind source Git or change GitHub.
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
a pending transaction. It does not rewind Git fast-forwards or delete caches.
Use a verified cached executable or ./install.sh recover if a crash occurred
before cw enrollment. Do not hand-edit ownership or recovery records.

If a v9 upgrade's recovery is itself interrupted after restoring the old command
link, `cw` may point to v7/v8 while the pending journal still requires v9. Read
`recovery_runtime.revision` from `STATE/journal.json` without editing it and use
the preserved executable at `STATE/runtime/releases/RECOVERY_REVISION/cw`.
Replace both placeholders with the recorded state directory and revision:

```sh
STATE/runtime/releases/RECOVERY_REVISION/cw --manager-protocol
STATE/runtime/releases/RECOVERY_REVISION/cw recover --dry-run
STATE/runtime/releases/RECOVERY_REVISION/cw recover
```

For a v9 upgrade, verify that the first command reports `cw-manager-v9` before
recovery. This executable uses its immutable locator even with the source checkout
absent. Recovery checks a preserved executable against its own immutable receipt,
so sealed v7/v8 runtimes remain readable without rewriting their records. Do not
use a legacy reader for a new six-key journal; `./install.sh recover` is an
alternative only while the checkout is available and the v9 release prerequisite
is met. Skill rollback retains the compatible manager.

```sh
cw uninstall --dry-run
cw uninstall
```

Uninstall restores adopted registrations/original model and shared memory values
and removes owned
registrations, global/PATH blocks, and the command link. It preserves source,
immutable caches, unrelated settings, credentials, and connectors. Reinstall
through ./install.sh. Status, recovery, rollback, and uninstall use installed
records even if source is dirty, invalid, or unavailable.
It preserves later unrelated config content and current permissions. If the file
was absent before the first config ownership was acquired, removal deletes it
only when restoration leaves zero bytes; later comments, whitespace, or settings
keep it in place.
