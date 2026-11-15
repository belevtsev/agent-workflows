# Use existing skills with Kitsu roles

Kitsu can attach skills you already keep in a repository, in your personal skill directories, or
in installed plugins to a role on Codex or Claude Code. It discovers local `SKILL.md` files and
keeps their original directories authoritative, including sibling scripts and references.
Discovery and resolution read files; they install nothing and execute neither skill code nor a
host or model process.

Role selections are optional. With no selection, Kitsu pins no extra skill and the host's other
native skills remain available. Selections add expertise to the role's instructions. They grant
no tools or permissions and change no write limit, independent review, or human approval rule.

## Discover and select

Start inside your repository with the host you use:

```sh
kitsu skills list --host codex
kitsu skills list --host claude --json
```

The catalog shows qualified ids and names; `--json` also gives source locations, hashes,
availability, metadata, and diagnostics. Copy an id from your own output. For example, if a repository has a skill at
`.agents/skills/project-review/SKILL.md`, its Codex id is
`codex:repo:.agents/skills/project-review`:

```sh
kitsu roles set reviewer --host codex --skills codex:repo:.agents/skills/project-review
kitsu roles --host codex
kitsu skills resolve reviewer --host codex --json
```

The role table now includes **Skills** alongside model, reasoning, and advisers. Resolve returns
the original `SKILL.md` paths and SHA256 hashes for a fresh worker brief. It reads the current
files every time; changing `SKILL.md` changes the hash in the next resolution. Each hash covers
that file's bytes; sibling scripts and references have no separate receipt hashes.

Use a comma-separated list for several skills. A unique short name can be selected, and Kitsu
saves its qualified id. When two sources have the same name, use the exact qualified id shown
by `skills list`; Kitsu refuses ambiguity rather than choosing one. Missing, invalid, or
host-disabled selections also fail before the worker starts.

## Personal defaults and repository overrides

Save a personal selection with `--global`:

```sh
kitsu roles set reviewer --host codex --skills codex:user:project-review --global
```

This example assumes that id exists in your catalog. Personal bindings live at
`os.UserConfigDir()/kitsu/roles.toml`: normally `$XDG_CONFIG_HOME/kitsu/roles.toml` or
`~/.config/kitsu/roles.toml` on Linux, `~/Library/Application Support/kitsu/roles.toml` on macOS,
and `%AppData%\kitsu\roles.toml` on Windows. Personal inventory and `--global` edits also work
outside a Git repository. The personal file holds only skill selections;
`--global` accepts no model or reasoning override.

A repository selection lives in its root `kitsu-roles.toml`:

```toml
version = 1

[codex.reviewer]
skills = ["codex:repo:.agents/skills/project-review"]

[claude.coordinator]
skills = []
```

For each host and role, a repository array replaces the whole personal list. An omitted array
inherits the personal list; an explicit empty array suppresses it. The coordinator can select
skills, while its model and reasoning remain settings of the host session.

```sh
# Clear pinned skills in this repository, including inherited personal selections.
kitsu roles set reviewer --host codex --skills ''

# Remove repository overrides; personal skills and built-in model defaults apply again.
kitsu roles reset reviewer --host codex

# Remove only this host/role's personal skill selection.
kitsu roles reset reviewer --host codex --global
```

An omitted `--skills` leaves its current override unchanged. Repository reset also removes that
role's model and reasoning overrides. These settings apply to newly started workers.

## Sources and host visibility

Kitsu searches supported local layouts for the chosen host. The inventory is a view of readable
files and observable configuration, not a guarantee that it exactly matches the host's current
effective catalog:

| Host | Repository skills | Personal skills | Plugin inventory |
| --- | --- | --- | --- |
| Codex | `.agents/skills/` from the current directory up to the repository root | `~/.agents/skills/` and legacy `$CODEX_HOME/skills/`, default `~/.codex/skills/` | Configured plugin ids in the local cache, with a known local install or one unambiguous installed version |
| Claude Code | `.claude/skills/` from the current directory up to the repository root | `$CLAUDE_CONFIG_DIR/skills/`, default `~/.claude/skills/` | `plugins/installed_plugins.json` version 1 or 2, with applicable user, project, or local installation metadata |

Codex reads user and applicable repository `.codex/config.toml` settings. Claude Code reads user
`settings.json` plus repository `.claude/settings.json` and `.claude/settings.local.json`.
Codex applies ordered `skills.config` name/path rules with repository settings after user
settings; `skills.bundled.enabled = false` disables bundled `.system` sources. Observable
disabled skills and plugin enablement apply. Claude `skillOverrides: off` is disabled;
`user-invocable-only` is manual-only. Unreadable or malformed supported settings prevent selection
when enablement cannot be verified.

Plugin ids include the namespace, for example `codex:plugin:review@my-market:skills/review`.
Codex inventories only configured plugin ids; multiple cached versions require an explicit root.
Live-session overrides, managed system policy, and cloud-synced or desktop-managed inventories
are not fully enumerated. Check `skills list --json` diagnostics when a source is absent or
disabled. Opaque catalogs may need an explicit directory:

```sh
kitsu skills list --host codex --root /absolute/path/to/existing-skills
kitsu roles set developer --host codex --root /absolute/path/to/existing-skills \
  --skills '<qualified-id-from-list>'
kitsu skills resolve developer --host codex --root /absolute/path/to/existing-skills
```

`--root` is repeatable and accepts a single skill directory or a collection containing skill
directories. For a custom source, selection saves its absolute `SKILL.md` reference so later
resolution can find the original without repeating `--root`. Prefer `--global` for these
machine-specific references. Shared receipts omit those paths. An explicit root exposes local
files; it does not assert that a
plugin is installed, enabled, trusted, or available to the host. Kitsu preserves observable host
disable settings and plugin enablement when it can read the supported configuration.

Metadata is parsed before selection. Codex requires `name` and `description`; Claude Code can
default the name from the directory and the description from the first nonempty instruction
line. Malformed frontmatter and unavailable entries are reported
with their source; they are not silently replaced by another skill. A skill with
`disable-model-invocation: true` is listed as manual-only and cannot be attached or resolved for
automatic role loading. Codex `agents/openai.yaml` can also make a skill manual-only with
`policy.allow_implicit_invocation: false`. An absent sidecar or policy field keeps the default;
a malformed or unreadable sidecar disables the source with a diagnostic.
`user-invocable: false` alone still permits use as background knowledge. Skill and metadata files
are bounded to 1 MiB, and scans to 4,096 directories and depth 12; reached limits produce warnings.

## Record a worker selection

Before each new role, the coordinator resolves that role's current selection:

```sh
kitsu skills resolve reviewer --host codex --task add-order-export --json
kitsu skills resolve reviewer --host codex --task fix-order-export --workflow small --json
```

With `--task`, the default workflow is `full`. Resolution appends an immutable version 1 receipt
under `openspec/changes/<task>/skills/<selection-id>.json`, or under
`openspec/changes/.skills/<task>/<selection-id>.json` for `small`. Commit these receipts with the
task. Full resolution requires an existing task folder. Receipts record the task, workflow,
host, role, selected ids, scopes, and source hashes without timestamps, machine absolute
paths or skill bodies. Without `--task`, resolve writes no receipt.

When promoting a small task, create its full task folder and resolve with `--workflow full`.
Kitsu moves the complete small skill history when the full skill destination is absent. It
refuses competing histories. `kitsu change check` validates receipt structure on both paths
without reading installed skill files.

The coordinator includes the selected ids, original paths, and hashes in the new brief and tells
the worker to load the original instructions, using a native skill tool where available or an
explicit read. Relative references stay relative to the original skill directory. Claude Code
role files include the `Skill` tool; Codex's host performs the execution and delegation.
The coordinator writes receipts before taking the worker's baseline for `kitsu roles check`;
workers do not write selection history.

A selection receipt is evidence of selection, not of loading or compliance. Its `load_status` is
`unverified`; no live host loading or behavior guarantee is implied. A later resolve reflects the
current source while retaining the earlier receipt. A shared receipt can be inspected without
the original local directories, but starting another worker needs those sources again.

## Bring language expertise through your own skills

Keep Kitsu's role texts generic and attach the expertise already used by your project. These
illustrative repository ids require your own `SKILL.md` files at the corresponding locations:

| Project | Example selection | Existing check command to describe in the skill |
| --- | --- | --- |
| Go | `codex:repo:.agents/skills/go-backend` | `go test ./...` |
| C++ | `codex:repo:.agents/skills/cpp-tests` | `ctest --test-dir build` |
| JavaScript | `codex:repo:.agents/skills/js-ui` | The project's existing `npm test` script |

For example:

```sh
kitsu roles set developer --host codex --skills codex:repo:.agents/skills/go-backend
kitsu roles set quality-engineer --host codex --skills codex:repo:.agents/skills/cpp-tests
kitsu roles set reviewer --host codex --skills codex:repo:.agents/skills/js-ui
```

Run only the checks the repository already defines. Discovering or selecting these skills does
not download a language pack, install a package, or run any of the example commands. See
[roles](roles.md) for path ownership and [the workflow](workflow.md#starting-a-role) for the brief.
