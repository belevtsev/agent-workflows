# ADR: language packs, Go first

Status: proposed. Design only.

## 1. Decision

Core is the `agent-workflows` plugin and `cw`. Each language is a plugin,
`agent-workflows-<lang>`, in the same marketplace. Evidence-rule code stays in
`cw`; a pack names its rule by id.

Rule: content goes in a pack when it tells the agent to use one language's
syntax, standard library, toolchain or libraries, or applies only to that
language's source. The rest is core. A core code sample may stay as an
illustration if the principle holds without it. Core names a slot ("the pack's
race command"), never a tool.

## 2. Inventory

| Skill | Class | Evidence |
| --- | --- | --- |
| backend-security-review | split | govulncheck, gosec rows (`references/security-evidence.md:9-13`) |
| clocks-and-time | split | DST and ordering neutral; `time.Now()` detail (`SKILL.md:18`) to `go-time` |
| code-review | core | no Go |
| codebase-design | core | Go only in samples |
| domain-modeling | core | no Go |
| expand-contract | core | no Go |
| go-pki-mtls | pack | Go TLS config (`references/identity-enrollment.md:7`) |
| go-principal-engineer | pack | "Go extension of principal-engineer" (`SKILL.md:8`) |
| graphify | pack | Go source only (`SKILL.md:3`); `cw graphify` stays |
| isolation-anomalies | core | SQL isolation; samples illustrate |
| observability | core | move `golang-error-handling` pointer (`SKILL.md:42`) |
| parse-dont-validate | split | "The Go recipe" (`SKILL.md:15-51`) to `go-types` |
| performance | core | move `golang-benchmark` pointer (`SKILL.md:89`) |
| principal-engineer | core | "language-neutral" in its description |
| production-plan | core | no Go |
| protobuf-contracts | core | Go is one peer among several |
| retry-amplification | core | samples illustrate |
| security-threat-model | core | no Go |
| shell-portability | core | about shells; Go test illustrates |
| software-architecture | core | no Go |
| task-orchestration | core | neutralise lines below |
| test-strategy | split | `references/go-fuzzing.md` to `go-fuzzing`; `references/e2e.md:59` (this repo's devcheck) to [development.md](../development.md) |

Make neutral, in [task-orchestration references][tor]:
`contracts/implementer.md:17`, `contracts/test-engineer.md:55-56` and its
"Extract Go declarations" section, `hosts.md:144-145`,
`global-workflow.md:44-57,80,249,355`, `consultation-helper.md:323`; and "Go-focused" plus the `go` keyword in core
manifests and marketplaces.
`agents/*.md` have no Go; the in-flight spec agents must not add any.

## 3. Layout

```text
packs/go/
  .claude-plugin/plugin.json   # agent-workflows-go; dependencies: ["cc-skills-golang"]
  .codex-plugin/plugin.json
  pack.yaml
  skills/  go-principal-engineer go-pki-mtls graphify go-time go-types go-fuzzing
```

Both marketplaces gain an entry with source `./packs/go`. Pack skills name core
skills, never `../` paths: a cached plugin gets no files from outside its
directory ([loading][load]).

`cc-skills-golang` stays a separate plugin, keeping its upstream sync and
licence. On Claude Code the pack depends on it; a bare name resolves in the
same marketplace and installs with the pack ([dependencies][deps]). Codex
documents no plugin dependencies ([build][cxb]), so `cw` installs both there.

## 4. `pack.yaml`

```yaml
schema: 1
id: go
detect: [go.mod, go.work]       # repo root or an ancestor of a changed file
commands:                       # argv, as cw task checks take them
  build: [go, build, ./...]
  test: [go, test, ./...]
  race: [go, test, -race, ./...]
  lint: [go, vet, ./...]
  format: [gofmt, -l, .]
tests:
  unit: {files: ["*_test.go"], argv: [go, test, ./...]}
  integration: {argv: [go, test, -tags=integration, ./...]}
  e2e: {argv: [go, test, -tags=e2e, ./...]}
interface_extract: [cw, task, contract-extract]
skills:
  implement: [go-principal-engineer, go-types, go-time]
  test: [go-fuzzing, "cc-skills-golang:golang-testing"]
  security: [go-pki-mtls, "cc-skills-golang:golang-security"]
review:
  - Every goroutine has an owner that waits for it and gets its error.
  - Changed concurrent code ran under the race command.
  - Reachable dependency vulnerabilities checked with govulncheck.
evidence_rule: go.package-test/v1
```

Versioning: `schema` is an integer. A new optional field keeps it; removing,
renaming or redefining a field bumps it. `cw` rejects unknown fields and
schemas, then applies the fallback rule with a recorded limitation. Rule ids
carry their own `/vN`.

## 5. Discovery at run time

- **Selection**: `languages.json` (version 1) in the `internal/userconfig`
  directory, written only by `cw lang`. Not `preferences.json`: older `cw`
  rejects unknown members there ([userconfig.go][uc]).
- **Manifests**: `cw` embeds `packs/*/pack.yaml`; pack and `cw` ship in one
  release. `cw` cannot use `CLAUDE_PLUGIN_ROOT`: Bash tool commands lack it
  ([reference][ref]).
- **Applies** when selected and a `detect` file exists. `cw task start` records
  the set; `Verify` asks the in-flight `internal/langpack` registry for those
  rule ids, else the fallback rule.
- **Orchestrator**: `cw doctor --summary` prints `packs: go (go.mod)`;
  `cw lang show go` prints commands, tests, skills and checklist.
- **Drift**: `cw doctor` compares the selection with `claude plugin list --json`
  ([cli][cli]) or Codex `config.toml` `[plugins."<name>@<marketplace>"]` ([build][cxb]).

## 6. Setup and changes

| Host | Choose or change |
| --- | --- |
| Claude Code | `/agent-workflows:setup`, or `claude plugin install agent-workflows-go@agent-workflows` then `cw lang add go` |
| Codex | `cw install --host codex --lang go` |
| Both | `cw lang list`, `cw lang add <id>`, `cw lang remove <id>` |

`/agent-workflows:setup` is a core skill (plugin skills run as
`/<plugin>:<skill>`, [marketplaces][mkt]). It asks for languages and runs
`cw lang add`, which runs `claude plugin install` unless
`claude plugin list --json` shows the pack ([cli][cli]). A plugin installs
others only through declared `dependencies` ([dependencies][deps]), and core
must not force Go. The
user types `/reload-plugins` ([cli][cli]). `cw lang remove go` runs
`claude plugin uninstall --prune -y`, which drops unneeded `cc-skills-golang`.
On Codex `cw lang` uses `internal/codexinstall`; then start a new session
([plugins][cxp]).

## 7. Migration, alternatives, risks, steps

**Migration.** `cw migrate-v2` selects `go` for upgraders. `cw doctor` prints
"go.mod found, Go pack not selected".

| Rejected | Why |
| --- | --- |
| One plugin, languages in `userConfig` | every user carries every language; nothing to remove |
| Core depends on the Go pack | forces Go on everyone |
| Merge `cc-skills-golang` | loses upstream sync |
| Pack ships an evidence script | runs plugin code in the gate |
| Parse `installed_plugins.json` | schema undocumented ([loading][load]) |

**Risks.** Codex has no documented dependencies, so `cw` owns two installs.
Selection can drift from the host; doctor reports it, tasks keep their start
set. Older `cw` ignores `languages.json` and uses the fallback.

**Steps.** New tests get a fresh test engineer.

1. Neutralise core text (section 2). Test: `internal/skillcheck` flags Go tool
   names in core outside fenced samples. Done: passes.
2. Create `packs/go`; move and split skills; marketplace entries. Tests:
   `claude plugin validate`; skillcheck rejects `../` leaving a plugin. Done:
   install in a temp `HOME` brings `cc-skills-golang`.
3. `internal/langpack/manifest.go`, embedded. Tests: valid, unknown field,
   newer schema, Go golden. Done: Go loads.
4. `internal/userconfig/languages.go`, `cw lang` in `internal/cli`. Tests: fake
   `claude` records argv; Codex in temp `HOME`. Done: idempotent add, remove.
5. Applicability in `cw task start`, `Verify`, `cw doctor`. Tests: `go.mod`
   present or not × selected or not; unknown rule id falls back. Done: gate
   uses the start set.
6. `cw install --lang`, `migrate-v2` default, setup skill. Test: temp `HOME`
   install. Done: both hosts reach the same set.
7. README: "Choose languages", "Change languages", "Language packs",
   "Included skills" split core and Go, migration note, "Go diagnostics" moved
   under the Go pack. CHANGELOG.

**Open questions.** (1) `gopls-lsp@claude-plugins-official` as a
cross-marketplace dependency ([dependencies][deps]) or a recommendation?
(2) Names `go-time`, `go-types`, `go-fuzzing`? (3) Move `internal/graphify`
to the pack later? (4) Third-party packs not embedded in `cw`?

[ref]: https://code.claude.com/docs/en/plugins-reference.md
[deps]: https://code.claude.com/docs/en/plugins/dependencies.md
[load]: https://code.claude.com/docs/en/plugins/loading.md
[cli]: https://code.claude.com/docs/en/plugins/cli-reference.md
[mkt]: https://code.claude.com/docs/en/plugin-marketplaces.md
[cxp]: https://developers.openai.com/codex/plugins
[cxb]: https://developers.openai.com/plugins/build/plugins
[tor]: https://github.com/belevtsev/agent-workflows/tree/main/skills/task-orchestration/references
[uc]: https://github.com/belevtsev/agent-workflows/blob/main/internal/userconfig/userconfig.go
