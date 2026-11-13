# Packs

A pack tells `cw task verify` which runs a changed task needs, for one
language or domain. `cw` has one pack today, `go`.

Packs are built into `cw`. [`embed.go`](../embed.go) embeds every
`packs/<id>/pack.yaml` at build time, and `cw` reads pack manifests only from
its own binary, never from an installed plugin or a file on disk. A pack names
its evidence rule by id, and the rule is Go code in
[`internal/langpack`](../internal/langpack). So the gate runs only code from
the `cw` release, and a pack and its rule always ship together. A pack cannot
ship a script, and a third-party pack cannot be added without a `cw` release.

The Go skills are separate from the Go pack. They come from the
`cc-skills-golang` plugin and a few Go skills in core; see
[Language packs](../README.md#language-packs) in the README.

## Commands

`cw lang` is an alias of `cw pack`. Each subcommand prints JSON.

| Command | What it does |
| --- | --- |
| `cw pack list` | Prints `selection` (`default` or `explicit`) and each built-in pack with what its `pack.yaml` declares except `schema` (`id`, `kind`, `detect`, `commands` when present, `evidence_rule`) and whether it is `selected`. |
| `cw pack add <id>` | Selects a built-in pack. An unknown id exits with status 2 and lists the built-in packs. |
| `cw pack remove <id>` | Deselects a pack. |

`add` and `remove` print `{"selected":[...],"written":...}`: the selection
after the change, and whether they wrote `packs.json`. On the default
selection they always write, which makes the selection explicit; on an
explicit one they write only a change. With `--dry-run` they print the
selection and write nothing.

## Selection

The selection is `packs.json` in the configuration directory: `--config-dir`,
else `$XDG_CONFIG_HOME/agent-workflows`, else `~/.config/agent-workflows`.

```json
{"version":1,"packs":["go"]}
```

- With no `packs.json`, every built-in pack of kind `language` is selected. A
  pack of kind `domain` is never selected by default.
- `cw pack add` and `cw pack remove` write an explicit `packs.json`. It
  replaces the default for all your repositories, even when it lists no packs.
  Delete the file to restore the default.
- The directory must belong to you with mode `0700`, and the file with mode
  `0600`. `cw pack add` and `cw pack remove` create a missing directory with
  mode `0700`.
- `cw` refuses a file with unknown members, without `"version": 1`, or that
  lists a pack twice.
- `cw task start` fails when `packs.json` names a pack this `cw` does not
  build in, and says to run `cw pack remove <id>`.

## How a task uses packs

1. `cw task start` reads the selection. It records each selected pack whose
   `detect` list names a file at the top of the selected path, not in a
   subdirectory. The task record's `packs` member holds the selection kind
   and, for each recorded pack, its id, the file that matched and its
   `evidence_rule`.
2. `cw task verify` applies the fallback rule first: at least one check run
   passed at the current source. Then it applies each recorded rule id.
3. A rule id this `cw` does not build in adds a limitation, and only the
   fallback rule applies for it.
4. A task keeps the packs and rules it recorded at start. A change to the
   selection or to a `pack.yaml` affects only tasks started afterwards.

The Go pack's rule, `go.package-test/v2`, needs, among other runs, a direct
`go test -race` run covering each changed package. See
[the Go package-test rule](operations.md#the-go-package-test-rule).

## `pack.yaml`

The schema is 1. [`packs/go/pack.yaml`](../packs/go/pack.yaml) is the example.
`cw` refuses unknown keys at any level, a file over 64 KiB or not in UTF-8,
and a file with more than one YAML document. It reads `schema` before any
other key and refuses a schema newer than the one it knows.

| Field | Required | Rule | What reads it |
| --- | --- | --- | --- |
| `schema` | yes | integer, `1` | the manifest loader |
| `id` | yes | `^[a-z][a-z0-9-]{0,31}$`, equal to the directory name | `cw pack`, the task record |
| `kind` | yes | `language` or `domain` | the default selection |
| `detect` | yes | 1 to 16 distinct file names, without `/` | `cw task start` |
| `evidence_rule` | no | a rule id such as `go.package-test/v2`, ending in `/v<N>` | `cw task verify` |
| `commands` | no | `race`: a non-empty argv that runs the pack's tests under the race detector | `cw pack list`, [`cw task init`](operations.md#starter-plan) |

A manifest holds only fields `cw` reads. Any other key, such as the
`tests`, `skills` or `review` of the [original design](design/language-packs.md),
is refused as unknown.

If one built-in `pack.yaml` is invalid, every `cw pack` subcommand and
`cw task start` fail with "built-in packs are invalid".

## Add a pack

Open a [pack proposal](../.github/ISSUE_TEMPLATE/pack_proposal.md) issue
first. [CONTRIBUTING](../CONTRIBUTING.md#adding-a-language-pack) has the
process. The technical steps are these.

1. Create `packs/<id>/pack.yaml`. `embed.go` picks it up; there is no other
   registration.
2. Choose the `kind`. A `language` pack is selected for every user who has no
   `packs.json`, as soon as they update `cw`. Its rule then applies to their
   new tasks. A `domain` pack applies only to users who run `cw pack add`.
3. Choose the `evidence_rule`. Without one, only the fallback rule applies.
   A new rule is Go code in `internal/langpack`: add its id to `builtInRules`
   and a case to `Evaluate` in `evaluate.go`, with tests. To make a rule
   stricter, add a new `/v<N>` id instead of changing the old rule, because
   open tasks keep the id they recorded.
4. Update the tests that pin the built-in set to the Go pack alone. Any new
   pack fails these:
   - `TestDefaultSelectionAndEmbeddedPacks` in `internal/langpack`;
   - `TestPackAddRemove` and `TestTaskStartRecordsPacks` in `internal/cli`;
   - `TestStartWithPacksRecordsPackSet` in `internal/taskflow`.

   A `language` pack also changes the default selection, so it fails
   `TestPackList` and `TestPackDefaultConfigDir` in `internal/cli` too.
5. Run `go test ./internal/langpack ./internal/skillcheck ./internal/cli ./internal/taskflow`.
   `TestRepositoryPacks` in `internal/skillcheck` checks that each pack loads
   and names a known rule.
6. Build `cw` and run `cw pack list` to see the new pack.

## History

[The original design](design/language-packs.md) proposed `languages.json`,
`cw lang show`, a plugin per language and a setup skill. None of those were
built. Read it as history; this page describes what ships.
