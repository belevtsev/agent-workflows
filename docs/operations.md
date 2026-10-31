# Installation operations

## Install and inspect

Use clean committed source and an existing Codex installation:

```sh
./install.sh --dry-run
./install.sh
cw status
```

The bootstrap obtains a verified executable, then Go performs the installation.
The default action is install; setup remains an alias. Mutations apply by default;
add --dry-run for a read-only local preview. A cold bootstrap preview reports
deferred validation without downloads or writes. cw status itself is offline.
The manager runs in Go. Installation uses an available Python 3.7+ with
virtual-environment support to prepare the consultation helper's isolated
Python/PyYAML environment; other skill-specific helpers retain their own
dependencies.

Status reports `model_profiles` with each configured model, reasoning effort, and
scope from the active snapshot. This is policy evidence. It does not identify the
model or effort used by an existing chat or establish account runtime availability.

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
two substantial-task adviser checkpoints and Codex's decision in a private external
task directory. `cw metrics schema` describes the versioned observation format;
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
selects the existing Codex directory; --state-dir selects device-local state.
Initial defaults use the user home, CODEX_HOME, and XDG_STATE_HOME. Subsequent
installed commands resolve their installation locator rather than the current
working directory. Explicit flags can override recorded paths.

```sh
./install.sh --home '/path/with spaces/home' --codex-home /path/to/codex --state-dir /path/to/state
```

State holds immutable releases/<SHA> snapshots, the active current pointer,
ownership/history, recovery records, and immutable manager runtime versions.
Skill registrations resolve through current. The source checkout remains editable;
installed snapshots are never edited directly. The manager version and active
skill revision are distinct, especially after rollback or --no-checkout.

The current manifest manages 26 registrations with 71 entrypoints, a marked block
in global AGENTS.md, the top-level model/model_reasoning_effort values, shared
memory defaults, and enrolled command/PATH
registration. It preserves unrelated content, comments, modes, credentials, and
repository instructions. Original settings and adopted registrations remain
reserved for uninstall. Modified owned content is a conflict.

## Shared Codex configuration

The optional version-one `global/codex-defaults.toml` resource delivers exactly
these five portable values through install and update:

```toml
[features]
memories = true

[memories]
generate_memories = true
use_memories = true
extract_model = "gpt-6.1-sol"
consolidation_model = "gpt-6.1-sol"
```

Its `version = 1` belongs to the source resource and is not written into the
device's config. Additional resource settings, missing keys, or incorrect value
types are rejected. Historical snapshots without the resource remain valid.
`cw status` reports shared ownership and the active snapshot's defaults in
`shared_config`.

The manager merges only owned value tokens into the selected Codex home's
`config.toml`. Device-specific paths, MCP configuration, project trust,
credentials, unrelated memory settings, comments, and current file permissions
are preserved. It records each shared key's value or absence when first acquired,
separately from the existing root-model ownership. Updating expected defaults
does not replace those saved originals.

Explicit `[features]` and `[memories]` tables and dotted keys are supported.
Inline parent tables such as `features = { memories = false }` or
`memories = { extract_model = 'local' }` are unsupported and cause a preflight
error before installation changes. Convert those parents to explicit tables or
dotted keys while retaining their other local values, then retry. Malformed TOML,
wrong owned value types, and a symlinked config also stop installation.

All involved root and shared values use one recoverable config-file operation.
Changes to an owned value cause a conflict. Independent local edits made before
rendering or during interrupted recovery survive the value merge.

## Consultation helper environment

For source snapshots containing the shared-defaults resource, applying install,
setup, or update prepares `HOME/.venvs/codex-decisions`, where HOME is the selected
`--home`. It creates the virtual environment with an available Python 3.7+ and
installs `PyYAML>=6,<7` through that environment's interpreter. A healthy existing
environment is reused. Missing or unsupported PyYAML is repaired inside the
existing environment. Dry runs do not create the environment or install packages;
historical snapshots without the resource skip this preparation.

Install/setup/update results include `consultation_runtime` with the environment
path, interpreter path, and status. `ready` confirms the selected environment was
verified or prepared; `deferred` describes a dry run, and `not_required` describes
a historical snapshot without the resource. Provisioning or repair sets the
overall result's `changed` value even when the skill snapshot remains the same.

Provisioning installs no system packages, changes no shell activation, and does
not change which Python the Codex app selects. Invoke the helper with the isolated
interpreter directly:

```sh
~/.venvs/codex-decisions/bin/python -B ~/.agents/skills/task-orchestration/scripts/consult_decisions.py --request /path/to/request.json --output /path/to/new-record.json --dry-run
```

For a custom home, replace `~` with that selected home. If Python is unavailable
or lacks virtual-environment support, preparation fails before managed activation
and reports how to supply Python 3.7+ with venv/ensurepip support on PATH and retry.
The virtual environment is outside immutable skill releases and remains available after rollback or
uninstall.

## Optional Graphify runtime

The first-party [Graphify skill](../skills/graphify/SKILL.md) uses the external
`graphifyy==0.9.91` package. Provision this optional runtime explicitly when
activating Graphify on a device. The guarded runner supports macOS and Linux.
Python 3.14 with `venv` and `ensurepip` is the recommended interpreter for this pin:

```sh
python3.14 -m venv "$HOME/.venvs/codex-graphify"
"$HOME/.venvs/codex-graphify/bin/python" -m pip install 'graphifyy==0.9.91'
"$HOME/.venvs/codex-graphify/bin/python" -m pip check
```

Use an explicit path to a suitable Python interpreter if `python3.14` is not on
PATH. This environment is separate from `codex-decisions`; leave the system and
Codex-selected Python defaults unchanged. Generic install, update, validation,
and status do not install Graphify, invoke its CLI, or create graph caches.
`cw status` reports optional prerequisites offline; detected executables do not
prove version compatibility or a complete graph.

Use the guarded helper from the installed skill:

```sh
~/.venvs/codex-graphify/bin/python -B ~/.agents/skills/graphify/scripts/graphify_runner.py build --repo /path/to/repo
~/.venvs/codex-graphify/bin/python -B ~/.agents/skills/graphify/scripts/graphify_runner.py query --repo /path/to/repo 'Which packages and symbols relate to the installation transaction?' --budget 2000
```

For a custom installation home or registration, substitute the actual interpreter
and catalog paths. The helper snapshots eligible tracked Go source and module
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
the other results. `--without-openai` is rejected for this policy. Installation,
local validation and status do not require credentials or make hosted requests.

`cw status` reports `openai_credential_present` from its own environment without
calling OpenAI. A terminal export does not update an already running Desktop
process; use the [setup instructions](../README.md#set-up-openai-decisions)
and a fresh process/chat. The helper never reads macOS launchctl or another
process's secrets by itself.

A snapshot with three required providers needs the separate
`cw-three-adviser-v1` manager capability. Bootstrap retains every prior capability
check, including `cw-openai-decisions-v1` for older optional snapshots. A published manager lacking a required
capability is refused before owned state changes. For unreleased committed source,
use the supported [Go development installation](development.md#distribution)
with explicit source/home/Codex/state roots. Existing sealed ownership and recovery
records are unchanged. See the [consultation record contract](../skills/task-orchestration/references/consultation-helper.md)
for unavailable/refused coverage, mandatory version-three records, and offline
adjudication of preserved historical records.

## Direct cw command

The managed ~/.local/bin/cw link executes the Go binary beneath the selected
state directory. It never calls install.sh. A PATH block is added only when
necessary. Use --shell bash, --shell zsh, or --shell none during install to
override shell selection. Unknown shells and custom zsh startup locations are
reported for manual PATH enrollment. Existing unowned cw definitions/paths are
not replaced.

Open a new terminal after migration. An existing shell may retain the former
function: use unfunction cw in zsh or unset -f cw in Bash, then reload the matching
startup file. Retain unrelated shell/profile edits.

## Migration

Existing managed installations are read using their historical state and checksum
format. Run the new bootstrap with explicit source when migrating an installation
that previously used --shell none. Recover any pending old journal before
migration. Source-independent removal of an owned legacy block does not require
its old launcher to remain executable.

For unmanaged legacy registrations, opt in to their adoption explicitly:

```sh
./install.sh --migrate-from /path/to/previous-source --dry-run
./install.sh --migrate-from /path/to/previous-source
```

For the separately installed TypeSafe skill under the selected home's
.codex/skills/typesafe-ai, add --typesafe-legacy. Adoption verifies its exact
matching files and keeps an ownership-checked backup. Unrelated occupied or
dangling paths remain conflicts. Credentials are never migration inputs.

The known vendor-to-third_party resource move changes owned registration targets
through the journal. Historical snapshots/receipts stay intact. Rollback derives
targets and names from the older snapshot. Forward install/update may add new
registrations but refuses removal, rename, or arbitrary retargeting of existing
ones. Each addition must be absent under ~/.agents/skills and free of collisions
under both the selected Codex home and standard ~/.codex/skills. New names are
not adopted from occupied paths, including dangling symlinks, during ordinary
installation or update.

### Manager v3, v4, or v5 to v6

After a compatible cw-manager-v6 release is published, run these commands from
the clean checkout:

```sh
git pull --ff-only
./install.sh --adopt-personal-skills
cw status
```

The old cw update rejects manifest v2 during validation, before
acquiring the new runtime. This one-time bootstrap route upgrades without
uninstalling or losing adoption records. The policy changes to Sol 6.1 max for
coordinator/substantive work, Luna xhigh for bounded execution, and Luna high for
bounded evidence. These model defaults are retained from v5. The pack grows to
26 registrations and 71 entrypoints.

Bootstrap verifies platform, checksum and capabilities. It requires `cw-manager-v6`,
`cw-model-policy-v2-dual-choice`, and `cw-codex-defaults-v1`. If latest lacks a
required capability, it refuses
activation and preserves the old binary and owned installation. Retry after a
compatible release is published. Go separately requires an exact candidate runtime, downloading
its release or building source if unavailable. Building requires the Go version
in go.mod. An independently verified exact-commit local v6 binary can be used in
isolated development environments before publication.

Recover a pending old transaction before migration. If an interruption leaves cw
running an older manager during the first v6 activation, use ./install.sh recover
with the compatible v6 cached bootstrap binary. Before writing a v2 journal,
activation stages a verified v6 executable, sealed locator, and receipt beneath
STATE/runtime/releases/REVISION/. Without the checkout, invoke that exact
STATE/runtime/releases/REVISION/cw executable directly with `recover`. Its
immutable locator retains installation roots even if recovery has already
reversed the active runtime pointer. Custom roots can also be supplied explicitly
with --source, --home, --codex-home, and --state-dir. Preserve receipts and journals;
the old enrolled command cannot recover a v2 transaction.

### Older v6 managers to paired consultations

An older v6 manager cannot validate the added paired-provider policy fields and
therefore rejects ordinary `cw update` before preparing its replacement. After a
compatible release is published, use the bootstrap from a clean updated checkout:

```sh
git pull --ff-only
./install.sh --dry-run
./install.sh
cw status
```

The separate model-policy capability lets bootstrap refresh an outdated v6 cache
while preserving the existing v6 runtime, receipt, and journal formats. Dry runs
stay offline when that cache lacks the new capability and report deferred
validation. Existing adopted directories need no new adoption flag. The source
is still activated only by the exact-revision Go manager.

### Older v6 managers to shared Codex defaults

An existing v6 manager with paired-policy support can validate a source revision
containing the optional shared-defaults resource and acquire the new manager
through its normal update path:

```sh
cw update
cw update
cw status
```

The first invocation replaces the older manager. The second applies the shared
settings through the capable manager and records their independent ownership.
Subsequent updates are a no-op when the snapshot, owned values, and helper
environment match. The
updated bootstrap applies manager acquisition and defaults in one installation
from a clean updated checkout. Managers that also lack paired-policy support
still need the bootstrap route described above.

Shared config activation and recovery require `cw-codex-defaults-v1` in a verified
v6 executable. The transaction stages a capable recovery runtime before writing
its journal. Historical model-only states and journals retain their exact format
and remain readable without the new capability.

### Adopting standalone personal skills

Only install/setup accepts --adopt-personal-skills. A fresh installation with
absent destinations needs no flag. An existing standalone production-plan,
Archify, or Docker skill requires explicit adoption:

```sh
./install.sh --adopt-personal-skills --dry-run
./install.sh --adopt-personal-skills
cw status
```

Manifest v2 refers to personal-skill-origins.json. Its source-relative hashes,
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
rollback. Existing v1 snapshots, receipts, and journals retain their encoding;
pending legacy transactions must be recovered before adoption.

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
Returning to a previous snapshot with ultra restores its historical defaults
while retaining the v6 manager. Status then reports those restored model profiles.
Shared memory values follow the previous snapshot's resource. Returning to a
snapshot without that resource restores the original shared values or their
absence and releases shared ownership. The earliest config-file origin remains
recorded while root-model ownership remains, including when shared ownership was
acquired before root-model enrollment.
The target snapshot must have a subset of the active registration names. A path
absent from that snapshot is removed only if its recorded origin was absent and
its current link remains exactly owned. Adopted symlink/directory origins cannot
be dropped; their restoration remains an uninstall responsibility.
Install/update/rollback require a fresh Codex chat to observe new instructions.

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
