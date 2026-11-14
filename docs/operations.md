# Operations

Claude Code gets the skills and worker agents from the plugin in
`.claude-plugin/`, and Codex from `.codex-plugin/` and
`.agents/plugins/marketplace.json`. This page covers installing them and `cw`,
the Codex instruction block, paths, exit statuses, what each version promises
([stability](#stability)), the model policy lookup and adviser keys.

## Install

### Host requirements

Other pages link here rather than repeat these versions.

| Requirement | Claude Code | Codex |
| --- | --- | --- |
| Platform | macOS or Linux; Windows is not supported | macOS or Linux; Windows is not supported |
| Host version | 2.1.292 or newer, because the coordinator passes `effort` on every dispatch; that also covers `omitClaudeMd` for the reviewer and the test engineer (2.1.271). The session-start hook names an older client ([session start](hooks.md#session-start)) | No minimum is checked; the hooks were recorded on 0.167.2 ([recorded host runs](hooks.md#recorded-host-runs)) |
| `cw` on `PATH` | The release that matches the plugin version ([get `cw`](#get-cw)); the session-start hook names a mismatch | Same |
| Go | 1.27.1 or newer, to build `cw` and for the Go rule of `cw task verify` | Same |
| git | On `PATH`; the task record and the commit gate read it | Same |
| Hooks | Run once the plugin is installed | Run only after you trust them in `/hooks` ([Codex](#codex)) |
| Worker models | Aliases `opus`, `sonnet` and `haiku` from [model-policy.claude.yaml](../skills/task-orchestration/model-policy.claude.yaml); an alias resolves to the host's current release | The `sol` and `luna` profiles of [model-policy.yaml](../skills/task-orchestration/model-policy.yaml) name the models |

Under Claude Code the effective model or effort of a worker can differ from
what the coordinator passes, which is why the
[host mapping](../skills/task-orchestration/references/hosts.md) has it check
runtime metadata. `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` forces
`CLAUDE_CODE_SUBAGENT_MODEL`, or the inherited model if it is unset, instead of
the Agent `model` argument. `CLAUDE_CODE_EFFORT_LEVEL` overrides per-invocation
effort. Organization `availableModels` restrictions can also substitute the
inherited model. See
[Claude Code subagent dispatch](https://code.claude.com/docs/en/sub-agents).
The coordinator profile's effort is `high` by default; `settings.json`
`effortLevel` is the user's to set. `settings.json` rejects `max`; use
`/effort max` or `/effort xhigh` for a session when the user asks for it.

Codex has no per-agent switch to keep instruction files out of a worker: it
injects `$CODEX_HOME/AGENTS.md` and the project `AGENTS.md` into a spawned
agent even with `fork_turns: "none"`, as reported for Codex CLI 0.166.0 in
[google/elephant-goldfish-model#237](https://github.com/google/elephant-goldfish-model/issues/237)
(open), so a Codex reviewer or test engineer sees those files. The Claude Code
reviewer and test engineer set `omitClaudeMd: true` instead, so author priors
and coordinator instructions stay out of an independent review or test.

### Claude Code plugin

```sh
claude plugin marketplace add belevtsev/agent-workflows
claude plugin install agent-workflows@agent-workflows
```

Inside a session, `/plugin marketplace add belevtsev/agent-workflows` and
`/plugin install agent-workflows@agent-workflows` do the same. The Go skill pack
is a second plugin in the same marketplace, `cc-skills-golang@agent-workflows`.
The plugin provides the skills and the seven worker agents under `agents/`
(`agent-workflows:spec-writer`, `agent-workflows:architect`,
`agent-workflows:implementer`, `agent-workflows:test-engineer`,
`agent-workflows:reviewer`, `agent-workflows:github-worker` and the opt-in
`agent-workflows:adversarial-tester`). It does not
edit `~/.claude/CLAUDE.md` or `settings.json`. `cw install --host claude` is
refused with a pointer to the plugin.

### Codex

Codex takes agent-workflows in one of two shapes: the Codex plugin, or
[`cw install --host codex`](#codex-install-and-uninstall) from a checkout. The
plugin has two rows because Codex runs none of its [hooks](hooks.md#codex)
until you trust them. "Conventions" is the text of `global/AGENTS.md`, which
holds the definition of a substantial task and the Code Review Rules.

| Shape | Skills | Conventions | Definition of "substantial" | Commit and push gate | Session-start context | archify | Plugin options | Unverified |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Plugin, hooks trusted (recommended) | Every skill under `skills/`; the Go pack is the separate `cc-skills-golang` plugin | Added by the session-start hook; no `AGENTS.md` file is written | Yes, in the conventions | The pre-publish hook denies, and denies where Claude Code would ask, except that the asks about a task record that is damaged or from a newer `cw`, or a task store it cannot locate or read, let the command go ahead ([Codex](hooks.md#codex)); needs the hooks trusted in `/hooks` | The conventions, a line when the plugin and `cw` versions differ, a line suggesting `cw gate install` once per task open in a repository without git's hooks, and a cached `cw doctor` summary when `session_doctor` is on | No | Environment variables of the shell that starts Codex | Session start on `clear` and in spawned agents; the session-start fallback without `cw` on `PATH`; the `small_task_prompt` and `session_doctor` variables; the version line, which reads `.codex-plugin/plugin.json` |
| Plugin, hooks not trusted: skills only | Every skill under `skills/` | None | Only in the `task-orchestration` skill, once loaded | `cw publish` only | None | No | No effect | Whether the interactive CLI warns about untrusted hooks; `codex exec` does not |
| `cw install --host codex` | Every registration in `skills-manifest.json`: the skills under `skills/`, the Go pack, archify, typesafe-ai and the Docker skills | Managed block in `$CODEX_HOME/AGENTS.md` | Yes, in the block | `cw publish` only; no hook, so no trust | None: no hook | Yes | No effect: no hook reads them | No recorded Codex run of this shape; that Codex reads `~/.agents/skills` and `$CODEX_HOME/AGENTS.md` rests on its documentation |

**Recommended: the plugin, with its hooks trusted.** It is the only shape whose
gate covers a plain `git commit` or `git push`, and it loads the same
conventions at session start. Use `cw install --host codex` instead when you
need archify, typesafe-ai or the Docker skills, which the plugin does not carry:
the architect contract and the `software-architecture` and
`security-threat-model` skills draw their diagrams with archify. There only
`cw publish` applies the gate.

- **Gate.** In every shape `cw publish` applies the gate itself and refuses
  with exit 3 `open-record`. Where no hook runs, a plain `git commit` or
  `git push` goes unchecked. Without `cw` on `PATH` there is no gate: the hook
  lets the command through.
- **Trust.** Codex runs no plugin hook until you trust it, and `codex exec`
  does not say it skipped them. Codex runs `hooks/codex-hooks.json`, which
  `.codex-plugin/plugin.json` names. Per Codex's documentation, trust is
  recorded against the hook's hash, so trust the hooks again in `/hooks` when
  an update changes that file, as the update that added it did
  ([Codex hooks](hooks.md#codex)).
- **Plugin options.** Codex has no plugin configuration and sets no
  `CLAUDE_PLUGIN_OPTION_*` variable; the hooks read the environment Codex was
  started with. Unset, the gate and the prompt for unrecorded changes are on
  and the doctor summary is off. A recorded run turned the gate off with
  `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false`; the other two variables use the
  same lookup but were not run. No option affects `cw publish`.
- **Evidence.** The plugin rows come from runs recorded with codex-cli
  0.167.2 and 0.168.0 ([Codex hooks](hooks.md#codex), [recorded host runs](hooks.md#recorded-host-runs));
  the last column lists what they did not cover. The session-start context
  is added at startup and after each compaction, not on a resume.

To install the plugin:

```sh
codex plugin marketplace add belevtsev/agent-workflows
```

Then run `codex`, open `/plugins`, and install `agent-workflows` (and
`cc-skills-golang` for the Go pack); open `/hooks` and trust the two
agent-workflows hooks; start a new session afterwards.

#### Both at once

Not supported: use one shape. `cw install --host codex` does not detect the
plugin, so nothing stops you, and the two write different files (the plugin
lives under `$CODEX_HOME/plugins`, while `cw install` writes `~/.agents/skills`
and `$CODEX_HOME/AGENTS.md`), so neither overwrites the other. But they
overlap: every skill under `skills/` is installed twice, and so is the Go pack
when its plugin is installed; with the hooks trusted, the conventions load
twice (the block and the session-start context); and the plugin and the
checkout update separately, so the two copies can differ. How Codex lists a
skill installed twice is unverified.

### Get cw

`cw` runs on macOS and Linux; Windows is not supported (`cw` does not build
there). Go modules (`go.mod` at the top of the task's path) get the
package-level `go test` rule in `cw task verify`; other repositories need any
one passed check (see [evidence rules](#the-go-package-test-rule)).

Download `cw_<tag>_<os>_<arch>.tar.gz` (`darwin` or `linux`, `amd64` or
`arm64`) and `SHA256SUMS` from the
[v3.0.0-rc.2 release page](https://github.com/belevtsev/agent-workflows/releases/tag/v3.0.0-rc.2),
check the archive against `SHA256SUMS`, and put `cw` on your `PATH`. For example,
on an Apple silicon Mac:

```sh
archive=cw_v3.0.0-rc.2_darwin_arm64.tar.gz
grep " $archive\$" SHA256SUMS | shasum -a 256 -c
tar -xzf "$archive" cw
mkdir -p ~/.local/bin && mv cw ~/.local/bin/cw
```

On Linux, use `sha256sum -c` instead of `shasum -a 256 -c`. Or, with Go 1.27.1
or newer, build the same tag:

```sh
go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.2
```

The project owner pushes each release tag ([releases](development.md#releases));
the release page and `go install` work once `v3.0.0-rc.2` is tagged. It is a
prerelease, so GitHub's latest release stays `v2.0.96`, the last release of the
v2 installer. The old path without `/v3` gets no updates: its `@main` and
`@latest` queries fail with a module path mismatch.

Then check the build with `cw version`, which prints JSON with `version`,
`revision`, `built_at`, `os` and `arch`. Bare `cw` prints help.

Upgrade every `cw` that shares a task store at once, including the one the
commit hook runs. The first revision a newer `cw` writes to a task can raise
the record's version; an older `cw` then cannot read the record. This `cw`
writes version 5 records. A `cw` older than v3.0.0-rc.2 cannot read them, calls
them unsupported, and its hook, where it has one, skips such a task instead of
gating it. From v3.0.0-rc.2 on, a `cw task` command on a record from a newer
`cw` exits 4 and says to upgrade `cw`, and the hook asks before each commit and
push ([task record format](development.md#task-record-format)).

## Codex install and uninstall

`cw install --host codex` is the alternative to the Codex plugin: it links every
registered skill, including archify, typesafe-ai and the Docker skills, and
writes the instruction block, but installs no hook ([install shapes](#codex)).
Use one or the other, not both ([both at once](#both-at-once)).

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
it fail without changing anything. Remove that entry.

```sh
cw uninstall --host codex
```

`cw uninstall --host codex [--from DIR]` removes the links into the checkout and
the block, and deletes `AGENTS.md` when nothing else remains in it. Entries that do
not point into the checkout stay. It works after the checkout has been deleted.
Pass the same `--from` you installed with: when no link points into the selected
checkout, it still removes the block and prints a line saying so.

There is no state file, journal, snapshot or rollback, and no `--dry-run`.
`cw install --host claude` and `cw uninstall --host claude` are usage
errors that point at the Claude Code plugin.

## Paths

Global flags apply to every command:

| Root | Resolution |
| --- | --- |
| Home | `--home`, else the user's home directory |
| Codex home | `--codex-home`, else `$CODEX_HOME`, else `~/.codex` |
| Claude Code home | `--claude-home`, else `$CLAUDE_CONFIG_DIR`, else `~/.claude` |
| State | `--state-dir`, else `$XDG_STATE_HOME/agent-workflows`, else `~/.local/state/agent-workflows` |
| Configuration | `--config-dir`, else `$XDG_CONFIG_HOME/agent-workflows`, else `~/.config/agent-workflows` |

`$XDG_STATE_HOME` and `$XDG_CONFIG_HOME` must be absolute paths, as the XDG
Base Directory specification requires. A command that needs the state or the
configuration directory refuses a relative value with exit status 1, rather
than resolve it against the working directory and move the task store with
every `cd`; the commit and push hook asks instead
([task store](hooks.md#task-store)). A relative `--state-dir` or `--home`
resolves against the working directory; `--config-dir` must be absolute.

The task store is `<state>/workflow-tasks` and the cost history is
`<state>/metrics/history.jsonl`. The configuration directory holds the
saved adviser keys (`credentials.json`), `preferences.json` and `packs.json` (the
pack selection written by `cw pack add` and `cw pack remove`, where `cw lang` is
an alias of `cw pack`; absent means the default selection).

Each host keeps its instructions and skills in its own place:

| Concern | Codex | Claude Code |
| --- | --- | --- |
| Global instructions | `$CODEX_HOME/AGENTS.md` | `CLAUDE.md` in the Claude config directory |
| Personal skills | `~/.agents/skills` | `skills/` in the Claude config directory |
| Explicit skill invocation | `$skill-name` | `/skill-name` |

`--dry-run` is not a global flag. Only the commands that can preview a write
have it, after the command name: `cw consult` (a validated preview without
network access), `cw pack add|remove` (the new selection, without writing
`packs.json`) and `cw task init` (the starter plan's report, without writing
the plan). Every other command rejects it as an unknown flag with exit
status 2.

v2.x used the same state and configuration directories. v3 reads the saved
adviser keys (`credentials.json`) and `preferences.json` that v2.x left; keep
those. It does not read the task records in `<state>/workflow-tasks` that
v2.0.66 to v2.0.96 wrote, yet an open substantial one still gates commits and
pushes in its repository until its directory is moved out of that store
([which records gate](hooks.md#which-records-gate)). Before upgrading, finish
each open v2 task with `cw task verify`, or move its directory out of the
store. Anything else v2.x left behind can be removed by hand.

## Exit statuses

Every `cw` command exits with a status from one table, so a script or an agent
can tell "the answer is no" from "`cw` failed" without reading stderr:

| Status | Meaning | Where |
| --- | --- | --- |
| 0 | ok | The command did what was asked, including `cw task run` reusing a passed run ([reuse](#reusing-a-passed-run)). |
| 1 | failure | `cw` could not do it: invalid input, an I/O error, a damaged or locked record. A check that passed but whose log or test results `cw` could not write (`log_write_failed`, `test_results_write_failed`) is recorded as failed and exits 1. |
| 2 | usage | The command line is wrong: an unknown flag, a missing or conflicting option (such as a `cw task` command without `--task`), `cw task brief` with a stage outside the task's profile, an unknown subcommand (`unknown command "X" for "cw ..."`), or a positional argument the command does not take (`unexpected argument "X" for "cw ..."`). Nothing ran. |
| 3 | denied | The gate blocks the operation: `cw publish` `denied`; `cw hook git` blocking a commit or push. |
| 4 | refused | A precondition or the record's state refuses the operation: `cw publish` `refused`; `cw gate install` leaving a hook it did not write unchanged, and `cw gate install` or `uninstall` refusing a hooks directory that can serve other repositories (without `--shared`) or, for install, a missing one outside the git directory; every `cw graphify` `unavailable` report, whatever its `code`; `cw task assign` past a stage's round cap or a third fix round on an open finding or requirement without `--owner-approval` ([round caps](#round-caps), [fix rounds](#fix-rounds)); `cw task assign` and `cw task run` at the budget's stop ([budget stop](#budget-stop)); a `cw task` command on a record from a newer `cw` (upgrade `cw`), or on one without `packs`, `test_rules` or `baseline_manifest`, or that binds `go.package-test/v1` (start a new task). Other records `cw` cannot read, such as version 1 or 2, exit 1. |
| 5 | negative result | The command worked and the answer is no: `cw task verify` incomplete; `cw task run` with a check that did not pass (failed, timed out, changed the source, or left a process behind) or that `cw` would not run with an unverified `go` ([which go](#which-go-a-check-runs)); `cw consult` with partial or unavailable coverage, an unresolved adjudication or a bound adviser without its key. |
| 6 | unknown | `cw publish` cannot establish whether a step happened. |
| 7 | mismatch | `cw publish` read back a step that differs from the request. |
| 130 | cancelled | The command was interrupted. |

On 5, `cw task verify` and `cw task run` still print the verification or the
check run on stdout, which names the reason. `cw consult` prints its status
line, or with `--summary` the one-line view, and writes the record to the
`--output` path. The task commands other
than `verify` and `run` still exit 1 when the record's state refuses them, for
example an abandoned task or a `cw task add-checks` refusal; a round past its
cap exits 4. The plugin hooks
(`cw hook pre-publish` and `cw hook session-start`) answer in JSON and exit 0
([hooks](hooks.md)). `cw hook git` exits 3 when the gate blocks the commit,
merge commit or push, which stops git ([git hooks](hooks.md#git-hooks)). `cw gate install` exits 4
when a hook it did not write is in the way, and still writes the other hooks;
`cw gate install` and `uninstall` exit 4 without changing anything when the
hooks directory can serve other repositories and `--shared` is not given
([installing the git hooks](hooks.md#installing-the-git-hooks)).

## Stability

From `v3.0.0`, `cw` follows [semantic versioning](https://semver.org) for
its core commands. `cw --help` lists the two groups. It leaves out
`cw graphify`, which needs the [optional Graphify runtime](#optional-graphify-runtime)
that `cw` does not install; `cw graphify --help` still shows its help.

| Group | Commands | Promise |
| --- | --- | --- |
| Core | `doctor`, `gate`, `help`, `hook`, `install`, `pack`, `publish`, `task`, `uninstall`, `version` | Their names, flags, [exit statuses](#exit-statuses) and JSON output, the plan and other JSON input formats, the [task record format](development.md#task-record-format) and the hook decisions change incompatibly only in a major release. |
| Experimental | `consult`, `graphify`, `keys`, `metrics` | Any of these may change or go away in a minor release. `cw task consult` and the consultation record it keeps belong to `cw consult`, so they are experimental too. |

Before `v3.0.0`, a release candidate may still break the core commands; the
[CHANGELOG](../CHANGELOG.md) lists each such change under Breaking.

The other version numbers follow these rules:

- **Plugin version.** The Claude Code and Codex plugins carry the version of
  the `cw` release they ship with, the tag without its leading `v`. Use the
  same version of both: at session start the plugin adds a line when the
  two differ ([session start](hooks.md#session-start)).
- **`pack.yaml` `schema`.** An integer, now 1. A `cw` reads the schemas it
  knows and refuses a newer one rather than read it partly; a key or value an
  older `cw` would refuse raises it ([`pack.yaml`](packs.md#packyaml)).
- **Task record `version`.** Raised on every change that a `cw` reading the
  previous version could not load; a `cw` reads its own version and older
  ones down to 3. A raise is listed under Breaking
  ([task record format](development.md#task-record-format)).
- **Skill wording.** The text of the skills, agents, references and
  `global/` conventions is outside semver: any release may reword it. What
  `cw` reads or prints, such as the model policy members `cw` reads, follows
  the rules above.

## Model policy

`cw consult` and `cw task start` select the model policy in this order:

1. `--policy FILE`;
2. `$CW_MODEL_POLICY`;
3. `~/.agents/skills/task-orchestration/model-policy.yaml`, when anything exists at
   that path (a path that exists but cannot be read is reported, not skipped);
4. the policy embedded in `cw`.

Only `--policy` opts in to the providers its policy enables. Under a policy
selected any other way, only the advisers you opted in to with `advisers.enabled`
or `cw keys set` take part ([adviser keys](#adviser-keys)).

The runtime reads only `version`, `consultation.deadline_seconds` (an integer
from 1 to 3600) and `consultation.providers` from it. The worker routing in
[model-policy.yaml](../skills/task-orchestration/model-policy.yaml) and
[model-policy.claude.yaml](../skills/task-orchestration/model-policy.claude.yaml)
is read by the coordinator through the skill, not by `cw`.

## Global project diagnostics and evidence

`cw doctor --repo /absolute/project/path` selects the project directly, without
requiring a clean project. It reports the target worktree/content identity,
discovered capabilities, repository contract paths, optional tool executables,
adviser key presence and whether git's own hooks run the commit gate
(`git_hooks`, [installing the git hooks](hooks.md#installing-the-git-hooks)). `--live` explicitly enables bounded local integration checks;
ordinary metadata does not prove semantic retrieval, dirty-source freshness or
runtime call targets. Each live probe and each Git inspection runs in a process
group of its own; when it answers, exits or reaches its deadline, `cw` kills the
group until no member is alive, and a member that survives fails the probe.
Missing or unsupported tooling is qualified and retains normal source discovery. Neither mode installs dependencies, changes watcher enrollment or
calls advisers. Saved-file tools cannot see unsaved editor buffers. Graphify
stays an optional map of tracked Go with its stale-cache guard. Coverage limits
and unknown checks stay visible in the report. A multi-module Go project can
need module-local checks.

`--summary` prints one line. Its `problems` lists the required checks that are
missing, degraded or stale, which block the workflow: `git_cli` and `cw_cli`
(git and `cw` on `PATH`), `workflow_skills` (the three core skills, in a skill
root or the agent-workflows plugin in Claude Code's or Codex's plugin cache),
`project_source`, `project_contracts` and `git_hooks`. Only those, and a warning,
make `status` `attention`; otherwise it is `ok`. `optional_missing` counts the
optional checks (Codex and Claude Code configuration, credentials, MCP servers,
gopls, Graphify, jbcontext) that are missing, degraded or stale, and
`--summary --all` lists them under `optional`. The full report, without
`--summary`, keeps every check.

`cw task` keeps source-bound requirements, actual argv check execution, references,
the adviser binding fixed at `cw task start`, two substantial-task decision
checkpoints (unless the plan declares them not required, with a reason that
verify lists) and the coordinator's decisions in a private
external task directory, by default `STATE/workflow-tasks`. Every task command's
`--task` takes the directory `start` printed or the task's ID (32 hex digits), which
names that directory in the task store: `$CW_TASK_STORE` when set (as for a task
started with `--store DIR`), else `STATE/workflow-tasks`; write `./ID` for a directory of that name. Its `start`, `assign`,
`handoff`, `accept`, `assess`, `consult`, `run`, `verify` and `add-checks` commands print a
one-line result by default and the whole record with `--full` (`run`: the check run's status and log, plus failing
test names, from a `go test -json` run's per-test results when it recorded them, and first error lines when it failed; `verify`: without its fixed
limitations, with problems that differ only in a requirement, assignment or check ID joined into one line,
and at most 20 problem lines, the rest counted as `omitted_problems`); `--summary` is still accepted there and changes nothing.
The `--help` of each of these commands, and of `usage`, ends with an example. `cw task schema <start|assign|handoff|accept|assess|add-checks>`
prints the JSON Schema of that command's JSON input: every member and its type, while the command checks further rules.
`cw task trace`, `cw doctor` and `cw consult` accept `--summary` for a one-line view. `cw task show` prints one line with the
task's state (each requirement's assessment, each check's latest run status, each assignment's state, the stored
verification in brief) and `fields`, the record's top-level members; `cw task show
--field <dotted.path>` prints only selected values and `--full` the whole record, `cw task trace` reports, without
changing the record, each plan requirement with the contract requirements a
test-engineer brief links to it, their mapped tests judged at the current source and
the `-json` check runs that recorded them, `cw task authorship --task DIR` lists
in one JSON line,
without changing the record and also for an abandoned task, each Go test file
changed since start with its authorship status (`independent`, `source_aware`,
`lost` or `unowned`), its reason and the assignment it rests on, plus problems
such as a missing or unreadable baseline manifest, and exits 0
whatever the statuses (1 when the task cannot be loaded), `cw task bundle` writes the
reviewer bundle a review assignment must carry (one reach row per changed file) to a
new file, and a failed write leaves no file at `--output`,
`cw task contract-extract` prints requested Go declarations without bodies for a
test engineer's contract, and
`handoff`, `accept` and `assess` fill omitted fields that have exactly one valid
value (task, assignment hash, agent, current snapshot, result hash, coordinator
and file hashes) while validating explicit values as before. `cw metrics schema` describes the versioned observation format;
`cw metrics summarize` reads supplied observations without changing project source.
`cw metrics import` reports per-agent tokens and dollars from a Claude Code transcript
or a Codex rollout, without writing anything unless `--append` asks it to add the
session to the [cost history](#cost-history); with `--composition` and `--prices` it also
attributes each agent's input to its sources (Claude transcripts only).
It labels an agent by its `cw task assign` brief, else by its description, else by
its agent type: `test-engineer` (or `cw-test-author` in older transcripts) is
`test_author`, `reviewer` (or `cw-reviewer`) is `review`, and `github-worker` (or
`cw-github-worker`) is `github`. A test engineer dispatched as `general-purpose` is
`test_author` only when its description or brief says "test author"; otherwise it
takes its brief's role. Two descriptions and types win over an `implementation`,
`evidence` or `verification` brief: a description naming GitHub work makes the
agent `github`, and on an `implementation` brief a description or brief text that
says "test author", or the `test-engineer` type, makes it `test_author`.
Unknown metrics remain unknown, and synthetic observations are never described as
production savings. With go1.27.3, a line that a test writes straight to stdout or
stderr starting with byte 0x16 becomes a `-json` result event for any test name it
gives, so it can forge a PASS; it cannot hide a real FAIL, because a FAIL of the same
test in a latest run at the current source makes that test `failed`. See [test evidence rules](#test-evidence-rules).

Source capture treats a path as a plain folder only when Git exits with a failure
status or no `git` is on `PATH`. Any other end of a Git probe stops the capture
with `Git inspection timed out after <budget> (git <probe>)`, `Git inspection was
killed before it finished (git <probe>)` (detected on Unix), `Git inspection
stopped by the caller (git <probe>): <reason>` or `Git inspection could not
complete (git <probe>): <cause>`; the last covers a child process that holds
Git's output open after Git exits. Each probe has 60 seconds, within the
caller's own deadline, and 10 seconds for its output after Git exits. After a
check runs, its source capture gets at most 20 of the 30 seconds for recording
the result, so the result is still written when that capture fails or runs out,
unless the task lock stays held through the rest; a check that passed but whose
capture failed or ran out is recorded as `source_changed`.

`cw task timeline --task DIR [--last N] [--summary]` reports the wall time a task spent, from
timestamps the record already stores, and is read-only: it loads the record with
the same validation as `cw task show` and writes nothing. It prints one JSON object with `task`, `task_id`, `started_at` (the
record's `created_at`), `updated_at`, `elapsed` and `entries`. `elapsed` runs from
`started_at` to `updated_at`, the record's last stored revision, not to the current
time, so it does not grow while the task sits idle. `entries` holds one entry per
worker assignment (`kind` `assignment`, `id`, `stage` = the brief's role, `agent`,
`state` = the latest acceptance's status, else the result's status, else
`assigned`, `assigned_at`, `submitted_at`, `accepted_at`, `assign_to_handoff` and
`handoff_to_accept`) and one per check run (`kind` `check`, `id`, `stage` = the
plan check ID, `state` = the run's status, `started_at`, `finished_at` and
`duration`), ordered by start time, with entries lacking a start last.
Timestamps are UTC RFC 3339 with nanoseconds and durations are rounded to the
millisecond (`250ms`, `1m2.003s`). A timestamp the record does not hold (no
handoff yet, no acceptance, a check still running) is `n/a`, and so is every
duration computed from one; a missing value is never shown as `0s`. Entries with
equal starts keep the order they were recorded in. After the clock steps back,
stored times run ahead of it until it catches up
([task record format](development.md#task-record-format)), so runs made meanwhile
show equal starts and a duration of `0s`. It lists the
last 20 entries, or the last N with `--last N` (`--last 0` lists them all), and
counts the earlier ones as `omitted_entries`, a member it omits when none was left out. `--summary`
prints JSON Lines rather than a one-line result: one compact object for the task
(`task_id`, `started_at`, `updated_at`, `elapsed`, `omitted_entries`), then one per entry with its
start under `start` and its durations, omitting `task`, `submitted_at`,
`accepted_at` and `finished_at`.

### Publishing a verified task

`cw publish` commits a verified task's files, pushes the commit and, with
`--pr create` or `--pr edit`, creates or edits its pull request. It runs only
after `cw task verify` passed on the current source and no other open task record
gates the repository, commits whole files of the task's verified change set, or
of the part of it that `--path` names, reads every step back, and continues from
the observed state when the same command runs again. Once its checks pass, it
appends a `started` entry (`publication-NNNNNN.json`) to the task directory, and
a `finished` entry when it ends; the entries are hash-chained through
`previous_sha256` and written beside the task revisions, which it never
changes. The push sends only the commit to `refs/heads/BRANCH` of REMOTE, never
tags; the command never amends or force-pushes. `--summary` prints one line with
the members `status`, `reason`, `task_id`, `commit`, `remote_ref`, `pr_number`,
`pr_url` and `record` instead of the whole report. It needs Git 2.24 or newer.

The command takes flags only, so the remote and the branch are two flags,
`--push REMOTE --branch BRANCH`, not `--push REMOTE BRANCH`; the local branch
must be `BRANCH`. `--path` narrows the commit to files of the task's verified
change set (by the spelling `cw task bundle` prints); without it the whole
change set since `cw task start` is committed, whole files only. `--pr create`
or `--pr edit` needs all of `--repo`, `--base`, `--title` and `--body-file`;
`--pr none`, the default, takes none of them. The message file must end with a
newline and start with a non-blank subject line; the title is one line of at
most 256 characters. `--timeout` bounds each git and gh command (default 10m).

Its exit statuses and reasons follow. The coordinator's action on each status
is in the
[publication reference](../skills/task-orchestration/references/github-worker.md),
which also names the cases that go to the GitHub worker instead.

| Exit | Status | Reasons | What to do |
| --- | --- | --- | --- |
| 0 | `published` | none | Report the commit, remote ref and PR from the report. |
| 1 | `failed` | `git-missing`, `locked`, `task-unreadable`, `record-invalid`, `capture-failed`, `read-failed`, `record-write-failed`, `commit-failed` | Fix the cause and re-run. After `commit-failed`, check the index (`git ls-files -s`): intent-to-add entries for new files may remain. `locked` means another run holds `.publish-lock` in the task directory, or a `cw` older than v3.0.0-rc.2 left a `.publish-lock` directory there; a killed run of a later `cw` leaves no lock. |
| 2 | usage error | none | Fix the command line; nothing ran. |
| 3 | `denied` | `task-abandoned`, `task-not-verified`, `source-changed`, `open-record` | Verify the task on the current source, or, with the user's confirmation, abandon the record named in `open_records`. A record `cw` cannot read (from `cw` v2.0.66 to v2.0.96, from a newer `cw`, or damaged) cannot be verified or abandoned: as its `problem` says, upgrade `cw` for a newer record, else move its directory out of the task store. |
| 4 | `refused` | `not-a-repository`, `nothing-to-publish`, `path-not-verified`, `branch-mismatch`, `head-moved`, `hunk-scope`, `content-conversion`, `git-locked`, `remote-unknown`, `remote-split`, `remote-moved`, `not-fast-forward`, `gh-missing`, `repo-mismatch`, `pr-exists`, `pr-missing`, `pr-ambiguous` | Fix the precondition, or use the fallback. A refusal at the checks commits nothing; one at a later step keeps the commit or push before it (`steps`), so re-run once fixed. Remove a Git lock in `locks_left` only after checking that no git process runs in the repository. |
| 6 | `unknown` | `commit-unknown`, `push-unknown`, `pr-unknown` | Read the reconciliation in the report (`steps`, `locks_left`), then re-run the same command only while the authorization still covers it. After an unknown PR create, check the PR list first. A `commit-unknown` with locks left may be a definite failure behind a live git process's lock. |
| 7 | `mismatch` | `commit-mismatch`, `push-mismatch`, `pr-mismatch` | Inspect what was written: a hook or conversion changed the commit, the remote moved, or the PR differs. Never amend or force-push; repair it through the fallback. |
| 130 | `cancelled` | `cancelled` | Interrupted while no commit, push or PR command ran, or during a commit that left HEAD unchanged. Earlier steps may have completed (a commit, or a commit and its push): the report's `steps` shows what did. A 130 during the commit step can leave intent-to-add entries for new files in the index: the report's `intent_to_add` lists the paths passed, so check `git ls-files -s` before handing over to the fallback. Re-run the same command, which continues from the observed state, only while the authorization still covers it. |

A refusal (exit 4, status `refused`) never commits part of the file set, but it
can come after steps that are already done:

- **At the checks, nothing is committed or recorded.** One file refuses them
  all: a `--path` outside the verified change set (`path-not-verified`), a file
  whose staged state is neither `--expect-head`'s nor the verified one
  (`hunk-scope`), or a file Git would convert (`content-conversion`).
  `cw publish` then makes no commit and no push, runs no git command that
  changes the repository or its index, and appends no publication entry, so
  the report's `record` is empty.
  The report's `detail`, also printed to stderr as
  `agent-workflows: publish refused (REASON): DETAIL`, names the first such file,
  for example `u.txt is not in the task's verified change set`; with
  `--summary`, the JSON line carries only the reason.
- **At a later step, the steps before it stay.** Each step checks again before
  it runs, and refuses when, for example, HEAD moved (`head-moved`), a Git lock
  appeared (`git-locked`), the remote branch moved (`remote-moved`) or a pull
  request for the branch appeared (`pr-exists`). A commit already made, and a
  push already done, are kept, never amended or reset: the report's `steps`
  shows them as `done` and the refused step as `refused`, and the `finished`
  entry records the refusal. Run the command again once the cause is fixed: it
  finds the commit, and the push if done, `already-present` and continues.

Its known limitations:

- A new file that the user's ignore rules exclude only through `XDG_CONFIG_HOME`
  or `GIT_CONFIG_*` makes `git add --intent-to-add` fail on every run
  (1 `commit-failed`), because the source capture does not read those rules.
- With `core.autocrlf` set to `input` or true, globally or in the repository,
  every commit-set file that contains a CR byte, binaries such as PNG images
  included, refuses the publication as 4 `content-conversion`.
- A Git lock that a concurrent git process (an editor's `git status`) holds for
  more than about two seconds reads as a stale lock (4 `git-locked` before a
  step, or 6 `commit-unknown` after a failed commit or intent-to-add), so check
  that no git process runs before removing one, and treat such a 6 as unknown.
- A remote with a push URL for another repository, several URLs or an
  `ssh_config` host alias in only one URL is refused as 4 `remote-split`, and
  with a pull request any host alias is refused as 4 `repo-mismatch`.
- A remote that Git hands to a remote helper is refused as 4
  `remote-split`, since the helper picks where the push goes: a
  `<transport>::<address>` URL, a scheme other than `https`, `http`, `ssh`,
  `git+ssh`, `ssh+git`, `git` or `file`, in lower case, or a remote with
  `remote.<name>.vcs` set. `host:path`, `[user@host:port]:path` and local
  paths are accepted.

A task check cannot run `cw publish`, even after `cw`'s global flags:
`cw task start` refuses such a plan, and `cw task run` such a check in a
recorded plan, with "publication and deployment commands are outside task
checks". This is a guard, not a sandbox, since a check can still run another
program that publishes.

The shared instructions apply across projects. A first-party Go repository, a
Terraform repository and a Roblox project retain their own checks and operational
rules. Native task commands execute only explicitly selected argv; repository text,
measurement input and model advice do not authorize publication or deployment.
`cw publish` is the only command that writes to a remote, and only after verify
passed. The coordinator runs it under the user's authorization; cw never
authorizes a publication, and no flag or text in it stands for the user's
consent.

### Starter plan

`cw task init --title T [--repo DIR] [--profile lite|standard|strict] [--out FILE] [--json]`
writes a plan that `cw task start` accepts unedited, then prints the
`cw task start` command to run next:

```sh
cw task init --title "Add a greeting"
cw task start --plan plan.json
```

The plan is a substantial task with the title, the profile (`lite` by default,
written as the plan's `profile` member), one requirement `r1` that repeats the
title, and one check bound to it. The check is the `commands.race` of the first
selected [pack](packs.md) whose `detect` file is at the top of `--repo` (default:
the current directory); in a Go module it is `race`,
`go test -race -shuffle=on ./...`. Without such a pack the check is `test`, which
runs `false` and fails until you replace its argv. Under `lite` and `standard`
the plan sets `checkpoints.required` to false with a reason, which verify lists;
`strict` leaves the checkpoints required. `--out` defaults to `plan.json` in the
current directory, and `cw task init` never replaces an existing file (exit 1).
It prints text, or one JSON object with `--json`; with `--dry-run` it prints the
same and writes nothing.

### Listing tasks

`cw task list` prints the tasks in the task store (`$CW_TASK_STORE`, else the state
directory's `workflow-tasks`) that belong to the Git repository of the working
directory, or of `--repo DIR`, and to its main worktree when run in a linked one:
one line each, newest first, with the task ID, state, kind, last update (UTC) and
title, and a last line counting the hidden ones. A state is one of:

| State | Meaning |
| --- | --- |
| `open` | Readable, not verified, abandoned or stale. |
| `verified` | Its last `cw task verify` passed. The source may have changed since: the commit gate checks that, the list does not. |
| `abandoned` | Closed with `cw task abandon`. |
| `stale` | Not abandoned and untouched for 14 days, so it no longer gates commits. |
| `unreadable` | `cw` cannot load its record (damaged, from a newer `cw`, or from v2); `--json` gives the load error. |

By default it hides abandoned and stale tasks. `--all` lists them too and, without
`--repo`, lists every task directory in the store, adding a repository column.
Outside a Git repository, pass `--repo DIR` or `--all`. `--json` prints one object:
`tasks`, each with `task_id`, `directory`, `title`, `kind`, `root`, `updated_at`,
`state` and, for an unreadable task, `error`; and `hidden`, the number left out
when it is above zero. The command is read-only. When the commit gate names three
tasks and counts more, it points here.

### Profiles

`cw task start` fixes one profile in the task record, as it fixes the packs: the
plan's `profile` member, or `lite` when the plan names none. `start` writes the
resolved name into the stored plan and prints it as `profile` in its summary. The profile holds
the stages the task may assign, the author round caps per stage, the fix rounds
allowed per finding or requirement, and the budget in US dollars, with the share
(80%) at which the task is to stop taking new work. `cw task assign` enforces the
round caps ([round caps](#round-caps)) and the fix rounds ([fix rounds](#fix-rounds)),
and with `cw task run` the budget's stop ([budget stop](#budget-stop)).
A plan's `budget` member (`{"max_usd": 10}`) may lower the
profile's budget, never raise it; it may also set `stop_at`, the share at which
the task stops, up to the profile's 0.8. A higher value of either is refused and
no task is created. A later revision that changes the profile fails to load. A record
started before profiles existed has none, and no caps or budget apply to it.

The budgets are provisional: they are not measured yet, and the evaluation runs
replace them before v3.0.0. The budget counts dollars only; there is no token cap.

| Profile | Stages | Author round caps | Fix rounds per finding or requirement | Budget (USD), stop at 80% | Used when |
| --- | --- | --- | --- | --- | --- |
| `lite` (default) | `tests`, `implementation`, `test-review`, `code-review` | `tests` 2 | 2 | 15 | no open design choice |
| `standard` | `design`, `design-review`, `tests`, `implementation`, `test-review`, `code-review` | `design` 2, `tests` 2 | 2 | 40 | an open design choice or a public interface change |
| `strict` | `spec`, `spec-review`, `design`, `design-review`, `tests`, `implementation`, `test-review`, `code-review`, `adversarial` | `spec` 2, `design` 2, `tests` 2 | 2 | 100 | the user asks, or the change touches security, data integrity or concurrency |

`tests` runs in parallel with `implementation`. `cw task assign` requires an
implementation or review brief to name one of the profile's stages, and a later
round of an author stage to name the findings it fixes or the assignment it
repeats ([assignment briefs](#assignment-briefs)).
Each stage starts a fresh worker: `assign` refuses a brief with a stage whose
`agent_id` an assignment of another stage already used, unless the brief
`repeats` an assignment ([stage briefs](#stage-briefs)). Under `strict` the adviser
checkpoints also apply when the task binds advisers. A `spec` or `design` cap
counts revisions after the first draft; a `tests` cap counts every round.

#### Round caps

`cw task assign` counts the author rounds of each capped stage:

- The briefs of a stage assigned before any of them returned, those with
  neither `fixes` nor `repeats`, are one round, the first.
- Each brief with `fixes` is a round, a refreshed brief after an amendment
  included. A brief may not carry both `fixes` and `repeats`.
- A brief with `repeats` replaces the round of the assignment it names, so it
  is no new round; when that assignment does not count, the repeat takes its
  place.
- An assignment closed `unused` whose handoff was not `completed` returned no
  work and does not count; one whose completed work was closed `unused`, such as
  a superseded round, still does.
- A `tests` cap counts the first round; a `spec` or `design` cap does not, since
  the first draft is not a revision. `implementation` briefs (implementer fixes)
  and reviews never count.

A brief that would take its stage past the cap is refused with exit 4 and no
revision, and `cw` prints the rounds used, the cap and each finding or
requirement the brief `fixes`. Ask the owner; only their own words go to
`cw task assign --owner-approval "<the owner's words>"`, which records them on the
assignment in `owner_approvals`, one entry (`text`, `for`, `at`) per limit it
goes past, shown by `cw task show --field assignments`. The round cap's limit is
`round-budget`. A brief past two limits takes the flag once per limit, each as
`LIMIT=TEXT`, such as `--owner-approval "round-budget=<the owner's words>"`;
words for a limit the brief does not go past are refused.
With `enforce_verify` on, the plugin hook asks the user to confirm the words
before such a command runs ([hooks](hooks.md#owner-approval)).

#### Fix rounds

`cw task assign` counts, per finding or requirement, the earlier assignments whose
`fixes` name it, in any stage, as it counts rounds above: one that returned no
work does not count, and a repeat of it takes its place and its `fixes`. A
finding with `reopens` (an earlier finding, as `<assignment id>/<finding id>`)
counts as that earlier one. A brief that fixes a key with the profile's fix
rounds (2) behind it while the key is still open is refused with exit 4 and no
revision, naming every such key the brief fixes, its rounds and the profile's
allowance. A finding key is open while any finding of its `reopens` chain is
open: until the latest acceptance of that finding's result disposes it as
`resolved`, `rejected` or `deferred`. A requirement with checks is open while
one of them lacks a credited pass at the source the brief is assigned at, a
reused pass included: the assignment records that source's digest as
`source_sha256`, and load judges it the same way. A requirement without checks
is open until its latest assessment says `satisfied`. `cw` lists each key's
latest four rounds (assignment and the first line of its result summary, cut to
200 bytes, counting the rest) and names the owner's two choices, both the
owner's call: one more fix round, or a change of approach. Either way the owner
gives words, passed as `--owner-approval "escalation=<the owner's words>"`, that
approve the round or name the new approach; or the owner disposes of the
finding, rejected against evidence or deferred with the owner's reason. A
third `tests` round that is also a third fix goes past both limits and needs
words for each.

#### Stage briefs

`cw task brief --task DIR --stage S [--json]` prints what a fresh worker of
stage `S` starts from, so neither a worker nor the coordinator is continued
from an earlier context. It reads the record and writes nothing. The brief holds the task ID and title; the profile
and, for a capped stage, the author rounds left; the one role contract under
`skills/task-orchestration/references/contracts/` the worker loads; the stored
contract, bundle and input files of the stage's latest assignment, as absolute
paths; the task's check IDs; the plan requirements; and the stage's open
findings, newest first, each as `<assignment id>/<finding id>` (the form
`fixes` takes) with its severity, one line and the result's report file. A
stage's findings are those of its own results and of the review stage that
reviews it (`test-review` for `tests`, `code-review` and `adversarial` for
`implementation`); a finding is open until an acceptance resolves, rejects or
defers it.

The text, and the `--json` object, are each at most 4,096 bytes. Every entry is
listed whole or not at all: the title, files, checks, requirements and findings
each take at most an equal share of the room left when their turn comes, the
findings stop at the first that does not fit, and the last line (`omitted` in
JSON) counts what is not listed, such as `12 older findings`. A stage that is
not one of the profile's, or any stage of a task started before profiles, exits
2.

### Worker records

`cw task assign`, `cw task handoff` and `cw task accept` record the brief, the
result and the coordinator's acceptance of delegated work; the coordinator's
side is in [worker handoffs](../skills/task-orchestration/references/worker-handoffs.md).
`cw task schema <assign|handoff|accept>` prints each shape. This section lists
what the commands check.

#### Assignment briefs

An implementation brief, then a code review brief with its filled
[bundle](#review-bundle):

```json
{"id": "component-change", "agent_id": "/root/implementer", "role": "implementation",
 "stage": "implementation", "goal": "Implement the agreed component behavior", "owned_paths": ["src/component"],
 "constraints": ["Preserve unrelated changes and the agreed interface"],
 "check_ids": ["unit"],
 "stop_condition": "Return changes, source evidence, actual checks and unresolved questions"}
```

```json
{"id": "review-1", "agent_id": "/root/reviewer", "role": "review", "stage": "code-review",
 "goal": "Review the change against the requirements", "owned_paths": ["."],
 "constraints": [], "check_ids": [],
 "stop_condition": "Return findings with source evidence and what was not reviewed",
 "bundle": {"path": "/private/review-1-bundle.json", "sha256": "<sha256 of the filled file>"}}
```

The record retains the brief's task ID, exact source snapshot and SHA-256;
`assign` prints the assignment ID, its SHA-256 and the snapshot fingerprint,
and `handoff` fills the assignment hash and snapshot.

Under the task's [profile](#profiles), an
`implementation` or `review` brief names its `stage`, one of the profile's stages
(a test engineer's is `tests`, a test review's `test-review`, a code review's
`code-review`); `evidence` and
`verification` briefs need none. Several first-round briefs of one stage may run
in parallel on separate paths. Once a brief of a stage has returned a result, a
later `spec`, `design`, `tests` or `implementation` brief is a fix or a repeat:
it names `fixes`, each entry one `{"finding": "<assignment id>/<finding id>"}`
or `{"requirement": "<plan requirement id>"}`, or `repeats`, the earlier
assignment of the same stage it replaces. `assign` refuses a brief that lacks
them and names the member. A task started before profiles accepts briefs
without them.

Roles are `implementation`, `evidence`, `review` and `verification`; only
implementation results can declare changes. `owned_paths` holds clean relative
files or directory prefixes (`.` covers the whole project); active implementation
assignments cannot own overlapping paths. `check_ids` selects plan checks or
checks added by `cw task add-checks`. `cw task run` retains actual execution; a
worker's assertion is not a pass.

A brief of any role may carry `inputs`: up to 32 artefacts handed to the worker,
inside or outside the project, each as an absolute `path` with its `sha256`.
`assign` checks every hash and stores a private copy of each in the task, so a
later edit of the original changes nothing and `verify` reports a copy that is
missing or differs. A test engineer's brief, which carries `contract`, also
declares `requirements`: each contract requirement with the plan requirements it
implements as `criteria`, for example
`{"requirement": "R4", "criteria": ["inputs"]}`. They are required when the plan
has a `go test` check whose argv earns per-test credit, which needs `-json` in its
own argv and only flags the Go evidence rule allows (no `-args`, `-run`, `-skip`
or other refused flag), and refused when it has none. A correction round, a
refreshed brief after a contract amendment, or a refreshed round after a fix closed
the earlier round `unused` re-declares every requirement whose tests it owns, and
may map them to the files an earlier round wrote; `verify` lists a requirement no
current round declares, and fails a plan requirement that some closed round linked
when no current round links it; a plan requirement no round ever linked is no
problem and shows as `unmapped` in `cw task trace`.

Ownership checks cover direct file aliases, not hardlinks beneath separate
directory scopes. Scoped freshness hashes every entry under each owned
directory, rejects symlinks and stops at 30,000 entries or 64 MiB. `.` uses the
project's eligible-source snapshot.

#### Results and acceptances

The result JSON contains:

| Field | Meaning |
| --- | --- |
| `task_id`, `assignment_id`, `assignment_sha256`, `agent_id` | Exact task, brief and worker binding |
| `snapshot` | Full current `profile.snapshot` from `cw doctor --repo /absolute/project` |
| `status`, `summary` | `completed`, `blocked` or `cancelled`, with a concrete outcome |
| `evidence` | Source references with `path`, `start_line`, `end_line` and full-file `sha256` |
| `changed_files` | Declared changes with `path` and `sha256`, or `deleted: true`; empty for read-only roles |
| `check_ids` | Actual task check records cited by the worker |
| `findings` | Each finding's `id`, `summary`, `severity` (`blocker`, `major` or `minor`; required in a review; a finding without one reads as `major`) and source `evidence`; `[]` records none |
| `uncertainties` | Remaining unknowns; `[]` records none |
| `reach_runs` | Review only: how each reach-row mode was [run](#reach-runs) |
| `requirement_map` | Completed result of a brief with `requirements` only: for each declared requirement, its `tests` (`path` of a `_test.go` file this result or an earlier test-engineer result changed, and the Go test `name`), or an `uncovered` reason |
| `report` | Optional, task with a profile only: the full report's absolute `path` and `sha256` ([handoff caps](#handoff-caps)) |

`cw task` fills omitted single-valued
fields and validates explicit ones: `task_id`, `assignment_sha256`, `agent_id` and
`snapshot` (current source) in a result; `task_id`, `coordinator_id` and
`result_sha256` (the single recorded result) in an acceptance; `sha256` in any
source reference or changed file, from its current bytes. A filled hash binds the
bytes present when the record is written. Collections (`evidence`, `changed_files`, `check_ids`,
`findings`, `uncertainties`) are never filled.
`cw task trace --task <task-id> --summary` shows, for each plan requirement,
the mapped tests and the `-json` runs that proved or failed them.

An acceptance holds `assignment_id`, `status` (`accepted`, `rejected` or
`unused`), `reason`, real current source `evidence`, `check_ids` and, for every
finding, `finding_id`, a disposition `status`, `reason` and source `evidence`:

```json
{"assignment_id": "review-1", "status": "accepted",
 "reason": "Checked each finding against the current source",
 "evidence": [{"path": "src/component/store.go", "start_line": 40, "end_line": 58}],
 "check_ids": ["unit"],
 "findings": [{"finding_id": "F1", "status": "rejected",
   "reason": "Line 54 returns the last error after the final attempt",
   "evidence": [{"path": "src/component/store.go", "start_line": 51, "end_line": 55}]}]}
```

`cw task verify` lists a `deferred` finding as a limitation, and each rejected
`blocker` or `major` finding as a limitation (in its summary, first, as
`rejected_findings`); a rejected `blocker` is a problem until a later accepted
independent review whose bundle lists the rejection and whose result does not
re-raise its `id`. An `accepted` disposition acknowledges an unresolved finding
and blocks completion. An `unused` acceptance may give `[]` for `evidence` and
`check_ids`.

#### Handoff caps

In a task with a profile, `cw task handoff` refuses a result whose `summary`
exceeds 1,200 bytes, whose finding `summary` spans lines or exceeds 400 bytes,
or which encodes to more than 6 KiB without `changed_files`, `reach_runs` and
`requirement_map`; a reach run's `mode`, `command`, `observed` and `reason`,
and a requirement's `uncovered` reason, are at most 400 bytes each. A finding keeps its ID, severity, one line and source
`evidence`; the reasoning goes in a report file of at most 512 KiB that
`report` names. `handoff` stores a copy as `report-<assignment>.md` in the task
directory and prints that path as `report`; loading the task checks the copy
against its `sha256`. A task without a profile takes no `report` and keeps the
old limits.

#### Test authorship

`cw task start` records `"test_rules": "go.test-evidence/v1"` in every new
record; `cw` refuses a record without it (one from `cw` v2.0.66 to v2.0.96 or
a build before v3.0.0-rc.1). A record whose plan `kind` is `substantial` is *gated*:
`verify` and `assign` apply the rules below to it.

- **A contract needs a provable plan.** In a gated record, `assign` refuses a
  brief that carries `contract` when no plan check is a `go test` whose argv earns
  per-test credit (`-json` in its own argv, only allowed flags).
- **Proven PASS.** A mapped test passes only when a per-test credited run at the
  current source recorded at least one `run` event per result, no more `run`
  events than its `-count` times its `-cpu` values times the packages of its
  directory declaring the test, and no output line beginning with byte 0x16.
  Otherwise it is `pass_unproven`. This is a mitigation, not a proof: see
  [execute and record evidence](../skills/task-orchestration/references/global-workflow.md#execute-and-record-evidence).
- **gofmt hash.** For each changed Go test file (`_test.go`, outside
  `testdata`) of a contract-bearing result in a record with test rules, `handoff`
  fills `gofmt_sha256`, the SHA-256 of the gofmt-formatted bytes, when the file
  parses. An explicit value must equal it; on any other entry it is refused.
  Formatting a test engineer's file mechanically then keeps its authorship.
- **Authorship from the record.** Every Go test file (a `_test.go` file or a
  file under the `testdata` of a package that `go test ./...` runs at the selected
  path) whose bytes differ from the baseline must be `independent` or
  `source_aware`, or `verify` fails it as `lost` or `unowned`.
  - `independent`: a completed, contract-bearing result recorded its current bytes
    or deletion (or bytes gofmt-equivalent to them), its latest acceptance is
    `accepted` or `unused`, the round was never rejected, and its agent is not
    excluded.
  - An agent is excluded when it is the plan's `coordinator_id`, or holds any
    implementation assignment without a contract, any review or verification
    assignment, or a contract-bearing assignment whose result wrote a path that
    is neither a `_test.go` file nor under a `testdata` directory.
  - A round with any `rejected` acceptance never counts again, even after it is
    closed `unused`. A round closed `unused` without a rejection keeps its
    authorship.
  - `lost`: an independent round wrote the file, but its current state is neither
    what that round recorded nor gofmt-equivalent to it.
  - A mapped test whose file is not `independent` is `not_independent` in `verify`
    and `cw task trace`.
- **Reproduction tests.** A file is `source_aware` when cw reads this marking:
  a `resolved` finding disposition of a `review`, `evidence` or `verification`
  assignment cites the file in its evidence at the file's current bytes; the
  file is new (absent at start); the acceptance holding the disposition is the
  assignment's latest, is `accepted` or `unused`, lists at least one check in
  `check_ids`, and the assignment was never rejected. A
  source-aware file is never coverage: a mapped test in it is `not_independent`.
- **No go.mod.** When the selected path has no `go.mod` but has `go.work` or Go
  files, no file counts as a Go test file, and `verify` adds the limitation
  `test authorship not judged: ...`. A `go.work` workspace root is not supported.

`cw task authorship --task DIR` prints, read-only, each changed test file with its
status, reason and the assignment, agent, contract and finding the status rests
on. It exits 0 whatever the statuses and works on an abandoned task.

#### Review bundle

`cw task bundle --task <task-id> --review-id <id> --output FILE` writes, from
the record and current source, the requirements, prior findings with
their dispositions, each check's latest run with its log path, the files changed
since the task started (a manifest of path, mode and SHA-256, bound by
`manifest_sha256`), and one reach row per file changed since the previous reviewed
source: the latest review with a completed handoff that was not rejected, or the
task start for the first review. A row's hints give the file's shebang and up to 10
lines of docs and scripts that name it, invocations first. The coordinator fills
four facets per row from source, or writes `n/a: <reason>`:

| Facet | Answer from source |
| --- | --- |
| `callers` | Who calls, sources or imports the file; its entry points |
| `invocation_modes` | Every documented way it runs: piped (`curl … \| sh`), sourced, executed, flags, CI |
| `platforms` | Every operating system, shell and version it must work on |
| `consumers` | What reads the state, files, output or formats it writes |

`assign` regenerates the bundle and refuses a missing row, an empty facet or a bare
`n/a`, and any other difference, including a check run or record added after
`bundle`; it keeps an immutable copy for the next bundle.

#### Reach runs

`cw task handoff` refuses a completed review that carries a bundle unless each row
of a file that still exists has an entry in `reach_runs` and each entry names a
row; a `ran` entry needs `command` and `observed` and no `reason`, a `not_run`
entry `reason` and neither of the others:

```json
[{"path": "install.sh", "mode": "curl | sh under macOS /bin/sh (bash 3.2.57)", "status": "ran",
  "command": "cat install.sh | /bin/sh -s -- version", "observed": "exit 0, printed the version"},
 {"path": "install.sh", "mode": "Linux dash", "status": "not_run", "reason": "no Docker or Linux host"}]
```

A review assigned with a bundle from an older cw (`version` 1), which never
asked for runs, needs none. A review's handoff binds every check run and worker
record present when recorded; a later check run or record makes it stale.

### Cost budget

`cw task usage --task DIR (--claude-transcript FILE | --codex-session FILE)
[--prices FILE]` reads the session's transcript, read-only, as `cw metrics
import` does, and stores what the session has used since the task started in
the task record as a usage sample. Only requests stamped at or after the task's
`created_at` count, from the main transcript, its sidechain lines and its
subagent transcripts alike, so a session that began before the task counts
only its later requests. A sample keeps the session, its tokens (uncached
input, cache writes, cache reads and output, apart) and its dollars with their
source.

- **Dollars are estimates.** Each request is priced from its tokens with a price
  table: input, output and cache reads at their own rates, cache writes at the
  5-minute or 1-hour write rate from the request's own split. A write without
  that split is priced at the 1-hour rate, the higher one, so the budget never
  under-counts. A Claude Haiku 5.5 request whose prompt (its input, cache reads
  and cache writes) is over 100,000 tokens takes that model's long-prompt rates.
  The `cost-state` line Claude Code writes when a session exits is not used,
  whether it is missing, stale or does not match the requests.
- **The table** is cw's built-in one, dated 2026-11-13 and read from
  [Claude's pricing page](https://platform.claude.com/docs/en/about-claude/pricing),
  or the one `--prices FILE` names, which replaces it whole (same format as
  `cw metrics import --prices`, with an optional `date` and per-model
  `long_prompt`). A sample's `dollar_source` names the table's kind
  (`built_in` or `file`), its date and the SHA-256 of its bytes, and
  `usd_estimated` is true.
- **Unknown, never zero.** An incomplete or unreadable transcript, or a request
  without a readable timestamp, leaves tokens and dollars unknown with the
  reason. One request the table cannot price makes
  the session's dollars unknown: a model it does not list (dated model IDs
  included), a `speed` other than `standard`, or an `inference_geo` other than
  `global`. An unknown value is never counted as 0.
- **Task usage** is, per session, its latest sample, summed over the sessions;
  one sample of a session is enough. Within a session, every pair of consecutive known amounts
  must come from the same table and never fall; a different table, or a stale
  or truncated transcript copy that reads lower than the sample before it,
  makes the usage unknown.
- **Few revisions.** A sample is judged against its own session's stored
  samples. It is stored when it is the session's first, when its
  dollars turn known or unknown, change table, fall, or rise by at least 5% of
  the budget, and, while the session's dollars are unknown, when its tokens
  turn known or unknown, fall, or grow by at least 5%. A sample that moves the
  task's known dollars across the budget's stop or `max_usd` is always
  stored ([budget stop](#budget-stop)). The default output says
  whether it was stored and gives the task's usage; `--full` prints the record.
- A task started before profiles has no budget, and `cw task usage` refuses it.

#### Budget stop

The budget counts dollars only; tokens never stop a task. The plugin hook
samples the session before each `cw task` command, from a Claude Code
transcript or, under Codex, the session's rollout
([usage sampling](hooks.md#usage-sampling)). Where the hook does not run, the
skill runs `cw task usage` before each `cw task assign` and `cw task run`.
Codex keeps the rollout at `${CODEX_HOME:-~/.codex}/sessions/YYYY/MM/DD/rollout-<time>-<session id>.jsonl`,
dated the day the session started; take the file whose name ends with this
session's ID, or, when only this session runs, the newest:
`ls -t ${CODEX_HOME:-~/.codex}/sessions/*/*/*/rollout-*.jsonl | head -n 1`.

- **The stop** is judged on a known lower bound of the task's dollars: per
  session, the highest dollars any of its samples knew, summed. When that
  reaches `stop_at` (80%) of `max_usd`, `cw task assign` and `cw task run` take
  no new work: they exit 4 and write nothing. `cw` prints the usage, the budget
  and the share used, marks the dollars as estimated ("at least" when the rest
  is unknown), prints the [task receipt](#task-receipt), and asks: "continue
  with a higher budget, or stop with the partial result?" A sample that crosses the stop or `max_usd` is always
  stored, whatever the 5% rule says.
- **Continue.** Only the owner's own words go to `--owner-approval
  "cost-budget=<the owner's words>"` (or the words alone when the cost is the
  only limit passed) on that `cw task assign` or `cw task run`. Cost is
  task-level, so `cw` records them on the task as `owner_approvals`
  (`text`, `for: "cost-budget"`, `at`), not on an assignment. They raise the
  stop to 100% of `max_usd`, once; words given below the stop, or a second
  time, are refused. At 100% only a new task, with a higher budget, continues.
  A stored revision that adds an assignment or a check run past the stop the
  record's approvals set fails to load.
- **Stop.** `cw task verify`, `abandon`, `show`, `handoff`, `accept` and `usage`
  are never stopped, so the partial result can still be recorded and verify
  still reports on it. No check runs again, though: verify credits only the
  check runs made before the stop, or reused on unchanged source, so a source
  change after the last run leaves verify incomplete.
- **Unknown usage never starts a stop, and never lifts one.** With no sample,
  or a session whose latest dollars are unknown, and a known part below the
  stop, `cw task verify` lists "task usage is unknown: REASON" as a limitation,
  which does not block completion. A task started before profiles has no
  budget and never stops.

#### Task receipt

`cw task receipt --task DIR [--json]` prints what a task used and where it
stands. It reads the record only and stores nothing. `cw task verify` (pass or fail), `cw task abandon` and a
[budget stop](#budget-stop) print the same text after their own output, on
stderr, so their stdout stays one JSON document and their exit statuses do not
change. The receipt holds:

- the task ID, its state (`open`, `complete` or `incomplete` as the stored
  verification says, or `abandoned`) and its profile, or `none` for a task
  started before profiles;
- the author rounds each capped stage used against its cap, and the fix rounds
  each still open finding or requirement took against the profile's allowance;
- every owner approval, on an assignment or on the task (`cost-budget`), with
  what it was for, when, and the owner's words in quotes;
- the estimated dollars with the price table that priced them, against the
  budget and its stop, and the tokens with uncached input, cache writes, cache
  reads and output apart. Usage that cannot be measured reads
  `unknown (REASON)`, never 0; a known part above 0 is given as "at least";
- the checks planned, the runs executed and how many of them did not pass, and
  how many checks the stored verification credited by reuse (`n/a` before a
  verification);
- the findings counted by severity and status, the open ones (pending or
  accepted) listed newest first with one line each, and the wall time from
  `created_at` to `updated_at`, as [`cw task timeline`](#global-project-diagnostics-and-evidence) gives it.

The text, and the `--json` object, are each at most 4,096 bytes. Every entry is
listed whole or not at all: the open findings, then the fix rounds, then the
approvals each take at most an equal share of the room left when their turn
comes, and the last line (`omitted` in JSON) counts what is not listed.

### Task locks

Every `cw` command that writes a task record takes an exclusive `flock` on
`.lockfile` in the task directory. A second writer waits for it up to 10 seconds
instead of failing (an interrupt such as Ctrl-C ends the wait), and the kernel
releases the lock when its holder exits or is killed, so an interrupted `cw`
leaves the task writable. `cw publish` takes the same kind of lock on
`.publish-lock` without waiting: a second run exits 1 `locked`. Both files stay
in the task directory.

A `cw` older than v3.0.0-rc.2 used a `.lock` and a `.publish-lock` directory
instead, and a killed run left them behind. This `cw` still honours them: a
writer waits 10 seconds for `.lock`, then fails with `task is locked; inspect an
interrupted writer before removing its .lock directory`, and `cw publish` exits
1 `locked`. Remove one only after checking that no older `cw` runs. That `cw`
does not see `.lockfile`, so do not mix `cw` versions on one task. Once this
`cw` has published from a task, its `.publish-lock` file stays, so the
`cw publish` of an older `cw` reports `locked` on that task for good and says to
remove the file.

### What a check runs with

`cw task run` runs a check's argv directly, without a shell, in the selected
path, with no input and `timeout_seconds` as its bound. `cw task start`,
`cw task add-checks` and `cw task run` refuse some programs by name: shells,
`env`, `sudo`, network and deployment tools, git other than inspection commands,
and `cw publish`. That denylist is an accident guard, not a sandbox: `make`,
`python3 -c`, `go run` or a script can still do anything the user can
([threat model](../SECURITY.md#threat-model)).

The check gets `cw`'s environment without the variables whose names contain
`KEY`, `TOKEN`, `SECRET`, `PASSWORD` or `CREDENTIAL`, in any case, and without
`BASH_ENV` and `ENV`. A check that needs one of them, such as an integration
test that reads `DB_PASSWORD` or `GOOGLE_APPLICATION_CREDENTIALS`, names it in
its `env_passthrough`: at most 32 names matching `^[A-Z_][A-Z0-9_]*$`, each
matched exactly.

```json
{"id": "integration", "argv": ["go", "test", "-race", "./..."], "env_passthrough": ["DB_PASSWORD"]}
```

`cw` then passes that variable with its value when its own environment sets
it; every other variable the filter drops stays out. The plan records the
names, never the values, and `cw task verify` lists each check whose passed run
had them as a limitation, such as `check integration received DB_PASSWORD
through env_passthrough, where cw's environment set them`. A value the check
prints lands in its log in the task directory.

`env_passthrough` cannot name `BASH_ENV` or `ENV` (a shell that the check starts
would run the file they name), `GOROOT` or `GOTOOLCHAIN` (`cw` sets them for a
go check, [below](#which-go-a-check-runs)), or `GH_TOKEN`, `GITHUB_TOKEN`,
`GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN`: `gh` publishes with them,
and publication is outside task checks. `cw task start`, `cw task add-checks`
and `cw task run` refuse such a check. Like the denylist, this guards against
accidents: a test that calls the GitHub API can read a token under a name of
its own, which the plan and verify then show.

Variables the filter keeps, `GOFLAGS` among them, reach every check whether or
not `env_passthrough` names them. The run records `cw`'s `GOFLAGS` under
`goflags`, and the Go rule credits only allowlisted flags among those its go
command applied ([below](#which-go-a-check-runs)), so a `GOFLAGS` with `-exec` or
`-toolexec`, in the environment or in a go env file, earns no credit.

### Which go a check runs

A check whose program is `go` (or `go.exe`), named bare or by a path, runs only
a go command that `cw` verified. `cw` takes the `go` the check's `PATH` names
(or the path given) and runs it once as `go env GOROOT`, in the selected path,
with the check's environment and within its timeout, before it takes the task
lock. That follows a version manager's shim, a `golang.org/dl` wrapper and a
`GOTOOLCHAIN` switch to the go command that would really run; `cmd/go` looks
for a switched toolchain such as `go1.27.4` on `PATH` before it downloads one
into the module cache. `cw` then reads `$GOROOT/bin/go` without running it and
accepts it only when its build information names `cmd/go` and the printed
`GOROOT` resolves to that file's own tree, the directory above its `bin`. The
check runs that file by its absolute path with `GOROOT` set to that tree and
`GOTOOLCHAIN=local`, after the check's own environment, so neither a `GOROOT`
variable nor a go env file can pick another tree's `go.env`, and the go command
cannot switch again. The run records it under `tool` (`path`, `version`,
`sha256`, `goroot`), and `cw task verify` lists the go command and `GOROOT` that
each passed go check ran as a limitation. A run recorded before this has no
`tool`, and verify says so.

Next `cw` runs that file once as `go env -json GOFLAGS GOOS GOARCH GOHOSTOS
GOHOSTARCH CGO_ENABLED GOEXPERIMENT GOWORK GOAMD64 GOARM64 CC CXX FC CGO_CFLAGS
CGO_CPPFLAGS CGO_CXXFLAGS CGO_FFLAGS CGO_LDFLAGS`, with the
check's environment and in the selected path, and the run records the first
five values it printed under `go_env` (`GOFLAGS` redacted as in `goflags`);
the others go only into the run's `env_key` ([run keys](#what-a-check-run-records)).
The check then runs with that `GOFLAGS` (a single space when it is
empty), `GOOS` and `GOARCH` at the end of its environment, where they win over
a go env file, so a go env file changed after `cw` read it changes nothing;
the test binaries see them too. The Go rule credits a `go test` run only by that record: the
`GOFLAGS` the go command applied, the environment's or else a go env file's
(`GOENV`, `go env -w`), must pass the allowlist, and `GOOS` and `GOARCH` must
equal `GOHOSTOS` and `GOHOSTARCH`, since `go test` hands a test binary built
for another platform to a `go_<goos>_<goarch>_exec` program on `PATH`. A run
recorded without `go_env` (record version 4) earns no Go rule credit either:
run its check again. Verify lists such a run, and one built for another
platform, as `check <id> passed, but earns no Go evidence credit: …`.

#### The C toolchain of a go check

With cgo on (`CGO_ENABLED=1`, cmd/go's default when it finds a C compiler,
and needed by `go test -race` on Linux), go builds C code into test binaries
with the `CC` that `go env` reports, from the environment, a go env file or
the default `gcc` (Linux) or `clang` (macOS) on `PATH`; C++ files with its
`CXX`, Fortran files with its `FC` (`gfortran` when empty) and `.swig` files
with the `swig` on `PATH`. A wrapper compiler can add code that prints `PASS`
and exits 0 before any test runs. So `cw` judges that toolchain after the
`go env` read and records it on `go_env` under `c`: `cgo`, the programs `cc`,
`cxx`, `fc` and `swig` (`path`, `sha256`) and, when it did not verify,
`unverified` with the reasons. It verifies when:

- `CC`, `CXX` and `FC` are each one program without arguments, named bare and
  looked up on the check's `PATH`, or by an absolute path. `CC` must be found;
  `CXX`, `FC` and `swig` are judged when found.
- No program is a compiler launcher (`ccache`, `sccache`, `distcc`,
  `distcc-pump`, `icecc`, `buildcache`), by the name it is started by or the
  file it resolves to: a launcher runs the compiler its configuration chooses,
  or serves objects from a cache the user can write.
- Every step from `/` to each program, links included and walked as the
  kernel walks them (`..` after a link climbs from where the link led), is
  owned by root or another user, and no directory or file on the way is
  writable by its group or others. `/usr/bin/gcc` on Linux and the
  `/usr/bin/clang` shim on macOS pass; a program in a directory the user owns,
  or reached through a link there, does not.
- On Linux, `cw` runs in the initial user namespace (`/proc/self/uid_map` is
  `0 0 4294967295`): in a namespace such as `bwrap --unshare-user --uid 0`,
  the user's own files appear owned by root.
- On Linux, the assembler and linker the compiler names with
  `-print-prog-name=as` and `=ld` pass the same test. When it names a bare
  program, `cw` looks it up on `PATH` as gcc does (an empty entry is the
  directory the compiler runs in, a relative entry is taken there).
- `CGO_CFLAGS`, `CGO_CPPFLAGS`, `CGO_CXXFLAGS`, `CGO_FFLAGS` and
  `CGO_LDFLAGS` hold only `-O` and `-g` flags, as cmd/go's default `-O2 -g`
  does.
- None of `COMPILER_PATH`, `GCC_EXEC_PREFIX`, `LIBRARY_PATH`, `CPATH`,
  `C_INCLUDE_PATH`, `CPLUS_INCLUDE_PATH`, `OBJC_INCLUDE_PATH`,
  `CCC_OVERRIDE_OPTIONS`, `GCONV_PATH`, `LD_PRELOAD`, `LD_LIBRARY_PATH`,
  `LD_AUDIT`, `LD_RUN_PATH`, `SDKROOT`, `TOOLCHAINS` or a `DYLD_` variable is
  set, and on macOS `DEVELOPER_DIR` is unset or names the directory
  `xcode-select` chose.
- On macOS, the developer directory that `xcode-select -p` prints, where the
  `/usr/bin/clang` shim finds the toolchain it runs, passes the same test as
  a compiler: a user-owned `Xcode.app` does not.

An `FC` that does not pass leaves the toolchain verified but is not run.

The check then runs, at the end of its environment, with `CGO_ENABLED` as
read and, with cgo on, the `CGO_*FLAGS` as read, `CC`, `CXX` and `FC` set to
the paths judged, on macOS `xcrun_nocache=1` (xcrun then skips its per-user
lookup cache), and `COMPILER_PATH` naming the directories of an assembler
or linker found on `PATH`. A `CXX` or `FC` that was not found, or an `FC`
that did not pass, is set to a path under `/nonexistent`, so a package with
C++ or Fortran files then fails to build. A go env file or `PATH` entry
changed later changes nothing.

A run with cgo on whose toolchain did not verify still runs and can pass, but
earns no Go rule credit, and verify lists it as `check <id> passed, but earns
no Go evidence credit: cgo was on, and cw could not verify its C toolchain:
…`, followed by how to earn credit: run the check again with the system
compiler first on `PATH` and `CC` unset, with the variables named unset and
the `CGO_*FLAGS` holding only `-O` and `-g`, or with `CGO_ENABLED=0` where
`-race` is not needed. A run recorded before `cw` judged the C toolchain
earns none either: run its check again. With cgo off nothing is judged.

This shows who can change the toolchain, not what it does. When `cw` runs as
root, every file passes. On macOS, `cw` checks the `/usr/bin/clang` shim and
the developer directory it runs the toolchain from, down to that directory
only: not the files inside it, nor the assembler or linker there. The compiler's own temporary files, a `swig` added to `PATH`
during the run, a package's `#cgo` directives (which cmd/go checks itself)
and `pkg-config` are not judged.

When no go command verifies, such as a script named `go` first on `PATH`, a
`GOROOT` of links to the real tree, a broken shim or a toolchain that cannot be
downloaded, or its `go env` cannot be read, the check does not run:
the run is recorded as `failed` with failure `untrusted_toolchain`, no exit code
and an empty log, and `cw task run` exits 5 with an error that names the
candidate. Put the real `go` first on `PATH`. This shows what the binary is, not
that it reports honestly ([threat model](../SECURITY.md#threat-model)).

`cw task verify` reads the Go rule's facts, the package graph and whether
`-race` can run on the host, from a go command it verifies the same way: it
runs the `go` its `PATH` names once as `go env GOROOT`, offline
(`GOPROXY=off`) and within the facts budget, then runs `go list` from the
verified file with the same `GOROOT` and `GOTOOLCHAIN=local`. So a shim cannot
answer the `-race` probe with "not supported" to waive `-race`. When no go
command verifies, the facts are unavailable: verify lists the refusal, which
names the candidate, as a limitation, asks for a `go test ./...` run, and still
requires `-race`.

### What a check run records

Besides its log, exit code and the go command, each check run records two keys
in its start revision:

- `source_sha256`: the SHA-256 of the source manifest (path, mode and content
  hash of each eligible file; the mode keeps only the owner-executable bit)
  when the run started, the same digest a verified
  record stores as `verification.source_sha256`. Committing, staging or
  switching branches over the same bytes leaves it unchanged. The task
  directory keeps that manifest as `manifest-<source_sha256>.json`, written
  once per digest. A worker result records the same member for the source it
  was submitted at. When that manifest would exceed 8 MiB, the run or result
  is recorded without `source_sha256` and no manifest is kept.
- `modes_sha256`: the SHA-256 of each eligible file's path and full permission
  bits when the run started, which `source_sha256` does not cover.
- `env_key`: an HMAC-SHA256 of what the run's environment selects: for a go
  check, every value its `go env` read printed; for any check, the `GOFLAGS`
  of `cw`'s environment, unredacted, and each variable its `env_passthrough`
  names, set to its value or unset. Its key is a random
  32-byte secret of the task, kept in the task directory as `run-key-secret`
  (mode 0600) from the first run. No value is stored, and without that file a
  printed `env_key` does not confirm a guessed value, even a weak password.
  Within one task, two runs share a key exactly when those values are equal;
  keys of different tasks are not comparable.

Runs and results recorded before these members existed, or without them,
lack them, so they are never reused.

### Reusing a passed run

A passed check run stays creditable across commits, staging, branch switches
and merges that leave the source bytes as they were. A run at the current
snapshot earns credit as before. A run at another snapshot is reused when it is
the latest run of its check, passed with exit status 0 without the source
changing while it ran, its program is not `git`, and its `source_sha256`,
`modes_sha256`, go command (`tool`: path, version, SHA-256 and `GOROOT`),
`goflags` and `env_key` equal what the check would run under now. `cw task
verify` lists each reused run as a limitation, `check <id> reuses run
<run-id>, which passed at the same source digest, permission bits, go command,
GOFLAGS and environment key at commit <head>; it was not run again`, and `cw
task assess` binds it. `cw task run` then runs nothing: it prints the run's
summary with `"status":"reused"` and its `reason`, also under `--full`,
records nothing and exits 0. A worker result, a worker acceptance and the
requirement trace bind a reused run as they bind one at the current snapshot,
and a stored binding is checked again on load by its recorded
`source_sha256`. A requirement assessment or the decision that binds check
runs stays current across a commit of the same bytes while each run it binds
is still credited; one that binds no run stays tied to its snapshot. Verify,
assess, the handoff, the acceptance and the final context read the go
command, its `go env` and the key before they take the task lock, as `cw task
run` does.

Any one of these ends the reuse:

- a check whose program is `git`: its answer can depend on `HEAD`, the index
  or history, which `source_sha256` does not cover, so it is never reused;
- a change to an eligible source byte, path or owner-executable bit (a
  different `source_sha256`), or to any file's permission bits (a different
  `modes_sha256`);
- a different go binary or version, or another `GOROOT`;
- a different `GOFLAGS` in `cw`'s environment, for any program, compared
  redacted in `goflags` and whole in `env_key`;
- a different environment key: a value its `go env` read prints, such as a go
  env file's `GOFLAGS`, `CGO_ENABLED`, `GOOS` or `CC`, the path, hash or
  verdict of the C toolchain it judged, or a variable its `env_passthrough`
  names;
- a newer run of the same check that did not pass;
- a truncated log, or a log that is missing or differs from its hash;
- a run recorded without `source_sha256`, `modes_sha256` or `env_key`, or a configuration
  `cw` cannot read now (no verified go, no `run-key-secret`).

For a check whose program is not `go`, `cw` records no go command, so a
program it starts in turn, such as the go a `make` target runs, is not compared;
a script in the source is, through `source_sha256`. Any program that reads git
state, such as a script or `make` target that runs `git diff`, is reused
across a commit or staging of the same bytes: run it with `cw task run
--force`. Other variables `cw` passes
to a check are not part of the key: a test that
reads one of them, other than through `env_passthrough`, is reused when it
changes. Run it again with `cw task run --force`. `CW_NO_REUSE=1` turns reuse
off for every command: only a run at the current snapshot earns credit, and
`cw task run` always runs. A pass after a failed run of the same check at the
same `source_sha256` and `GOFLAGS` is a rerun and earns no Go evidence credit,
also across a commit of the same bytes.

### The Go package-test rule

A substantial plan binds at least one check to a requirement. Once its source
changes, `cw task verify` needs the latest run of some check passed at the
current source (the fallback rule). The Go rule applies on top of it when the
task applies the `go` pack, which is selected by default. A task records the
pack when `go.mod` or `go.work` is at the top of the selected path at start,
and the rule then checks packages whenever `go.mod` is at the top at verify. A
task that started with neither file gets no Go rule, even if it creates
`go.mod` later. `cw pack add` and `cw pack remove` affect only tasks started
afterwards; an open task keeps the pack set it recorded, and `cw pack list`
shows the selection. `cw pack remove go` writes an explicit `packs.json`, which
drops the Go rule for all of that user's repositories and leaves only the
fallback rule; `cw pack add go`, or deleting `packs.json`, restores it.

Under the Go rule (`go.package-test/v2`; a task keeps the rule it recorded at
start):

- Each changed package needs a check whose argv is a direct `go test -race`
  naming `./...`, the package (`./pkg`) or a parent (`./dir/...`). Where
  `-race` cannot run on the host (its own platform, with the cgo setting
  `cw task verify` sees), a plain `go test` run counts and verify records why.
- A file belongs to the package of its directory (or of the directory holding
  its `testdata/`); a non-Go file in a directory without Go files belongs to
  the nearest ancestor package.
- Each package whose test binary links a changed package needs a passed
  `go test` run covering it, unless only that changed package's `_test.go` or
  `testdata/` files changed.
- A changed `go.mod`, `go.sum`, `go.work`, `go.work.sum` or vendored file, a
  changed file with a build constraint line or a generated-code header, a
  changed embedded file, changed testdata outside any package directory, or a
  package graph that `go list` cannot compute at verify within 5 s needs
  `./...`. Verify runs `go list` offline (`GOPROXY=off`), inside the task
  lock, for at most 6 s, so a cold module cache can be the reason. It runs
  only a go it verified ([which go](#which-go-a-check-runs)), so a fake `go`
  first on `PATH` is another reason, and `-race` stays required.
- A pass after a failed run of the same check at the same source and
  `GOFLAGS` earns no credit until the source changes.
- Skipped tests are listed as limitations, which needs `-v` or `-json` on the
  run; a run without either gets a limitation saying they are not listed.
- Only runs whose every flag, in argv and in `GOFLAGS` (split as `go` splits
  it), is on the allowlist earn credit: `v`, `race`, `count` of at least 1,
  `timeout`, `cover*`, `tags` and the other flags in
  `internal/langpack/golang.go` that cannot skip or replace tests, with a
  `test.` prefix only on flags `go test` passes to the test binary. `-run`,
  `-skip`, `-args`, `-c`, `-n`, `-exec`, `-toolexec`, `-overlay`, `-ldflags`,
  `-gcflags`, `-asmflags`, `-C`, unknown flags, a pattern after a flag that
  follows a pattern (`./a -v ./b`, where `go test` hands `./b` to the test
  binary), wrappers (`make`, scripts), `GO.EXE` and Windows-style `.\pkg`
  patterns earn none.
- The rule reads the `GOFLAGS`, `GOOS` and `GOARCH` that the check's go
  command reports with `go env` just before the run, so a go env file's
  `GOFLAGS` (`GOENV`, `go env -w`) counts too, and a run for another
  `GOOS`/`GOARCH` than the host's earns none. With cgo on, a run earns none
  unless `cw` verified its C toolchain
  ([the C toolchain](#the-c-toolchain-of-a-go-check)).
- The run records `GOFLAGS` with compiler and linker flag values redacted, but
  stores the check's argv verbatim: keep secrets out of check argv.

### Test evidence rules

Every run records the per-test results of its `-json` events, read from its
complete output, as `test_results`. A `-json` run's stored log holds JSON event
lines, so its summary takes `failing_tests` from those results, while the error
lines and log tail still come from the log. Only a `go test -json` run with
flags the Go rule allows proves a mapped test. A `GOFLAGS=-json` in the shell
that runs `cw task run` reaches every check, and `go vet` then exits 0 on
findings, so `-json` belongs in argv.

For each current test-engineer assignment with a completed handoff, `verify`
judges every mapped test at the current source: it fails a test that is
missing, failed in a latest run at the current source, was only skipped, or has
no PASS in a latest passed run of a per-test-credited `-json` check; it lists
`uncovered` requirements and requirements no current round declares as
limitations, and fails a plan requirement whose every linking round is closed.
`cw task trace --task DIR --summary` shows each plan requirement's status and
gaps; without `--summary` it prints every mapped test with the runs that
recorded it.

In a plan with a per-test-credited `-json` `go test` check, `assign` refuses a
brief with a `contract` but no `requirements`; in a plan without one, it
refuses `requirements`. Records made before brief `inputs` and `requirements`,
result `requirement_map` and run `test_results` existed need none of them, and
load, hash and verify as before.

`cw task start` records `"test_rules": "go.test-evidence/v1"` in every new
record. A `cw` built before this rule cannot load such a record (its commit and
push hook skips it), so finish a task with the `cw` that started it. `cw`
refuses a record without `test_rules`, `packs` or `baseline_manifest`, or one
that binds `go.package-test/v1`, as `cw` v2.0.66 to v2.0.96 and builds before
v3.0.0-rc.1 wrote; an open one still gates until its directory is moved out of
the task store. The rules:

- **Proven PASS (a mitigation).** With go1.27.3, `-json` frames what tests
  print, but a line that a test writes straight to stdout or stderr starting
  with byte 0x16 becomes a result event for any test name it gives. So each
  run's results also count the `run` events and the output lines beginning
  with byte 0x16 of each test. A mapped test is `passed` only when a per-test
  credited current passed run recorded at least one `run` event per result, no
  more `run` events than its `-count` times its `-cpu` values times the
  packages of the test's directory that declare it, and no marked output line;
  a PASS without that is `pass_unproven`, which fails `verify`. A FAIL of the
  same test in a latest run at the current source makes the test `failed`
  whatever else the run recorded. This does not prove the test ran: code in
  the test binary can still print a forged `=== RUN` and `--- PASS` pair, or
  unmarked framing before the first test starts, for a mapped test that never
  genuinely runs (a subtest never created, a build-constrained file, `os.Exit`
  before `m.Run`), and a same-named declaration in another package of the
  directory that does not run the mapped name raises the allowance. Code
  review is the backstop.
- **Authorship (substantial plans).** Every changed Go test file must be
  `independent` (recorded by a never-rejected test-engineer round with a
  `completed` result whose latest acceptance is `accepted` or `unused`, by an
  agent that is not excluded) or a cited reproduction test (`source_aware`);
  `verify` fails a `lost` or `unowned` one, and a mapped test whose file is not
  `independent` is `not_independent`. When the plan has no per-test credited
  `go test -json` check and a changed test file is not `source_aware`, `verify`
  fails once with "test files changed, but the plan has no go test check whose
  argv earns per-test credit ...". An unreadable baseline manifest fails with
  `test authorship cannot be judged: <error>`. A selected path without `go.mod`
  but with `go.work` or Go files gets the limitation `test authorship not
  judged: ...`, and its test files are not guarded.
- **Contracts need a provable plan.** In a substantial task, `assign` refuses a
  brief with a `contract` when no plan check earns per-test credit.
- **Contract currency.** An earlier contract stops being re-extracted only when
  a later contract-bearing round that covers its owned paths has an `accepted`
  acceptance and no `rejected` one; a round never accepted, or ever rejected,
  leaves the earlier contract checked.

### What the final context binds

`cw task context --phase final` binds the source, the completed checks, worker
evidence and acceptances, and the coordinator's decision. Changing any of them,
even with the source bytes unchanged, needs a fresh final context and, when
checkpoints are required, a fresh adviser review. `cw task verify` rejects stale
source, missing requirements or checks, invalid references, incomplete
handoffs, unaccepted results, a missing or stale independent worker review, an
outstanding owned check, and missing, mismatched or unresolved checkpoints
(missing ones only when checkpoints are required). It lists each `deferred`
finding, and each bound adviser without a successful outcome, as a limitation;
with an empty binding it states that decisions rest on source evidence and
checks only. Hashes and structural validation establish consistency, not
semantic truth or complete test coverage. `cw task consult` validates the
adviser record against the binding; it makes no provider request, selects no
winning adviser and proves no cited semantics.

### Coverage profile of a check

`go test -covermode=atomic -coverpkg=./... -coverprofile=<path>` earns the same
credit as a plain `go test`. `-coverpkg=./...` also counts statements that one
package's tests run in another; an end-to-end test of a built binary still
records none. Write the profile to an absolute path outside the source tree,
such as the private directory that holds `plan.json` (the task directory does
not exist yet when the plan is written); a check runs without a shell, so the
directory must exist before `cw task run`. `cw task run` captures the source before and after a check, and
that source includes untracked files the repository does not ignore: a profile
written inside the tree changes the source during the run, so a passing run is
recorded as `source_changed`, and the profile then shows up as a changed file
in the reviewer bundle. Every run of the check overwrites the profile and `cw`
does not hash it. No coverage threshold applies and the profile gates nothing.
Tests that run the test binary again as a child process and compare its exact
output can fail under `-cover`, because the child warns on stderr that
`GOCOVERDIR` is not set. When the full check has such tests, keep it without
coverage and bind a second check that adds the flags and names the packages the
plan expects to change, leaving those tests' packages out; a change inside them
then has no profile, which the reviewer records under what was not reviewed.

### Check admission and stored history

The Git gate permits known inspection dispatch: `diff`, `status`, `log`,
`show`, `rev-parse`, `rev-list`, `ls-files`, `ls-tree`, `merge-base` and
`describe`. Standalone metadata outputs are `-v`/`--version`, `--html-path`,
`--man-path`, `--info-path` and bare `--exec-path`. Global directory and path
values are consumed as data. Explicit `-c` and `--config-env` overrides allow
only `core.quotePath`, `color.ui`, `diff.noprefix`, `diff.mnemonicPrefix`,
`diff.context`, `status.short` and `status.branch`, compared
case-insensitively. Unknown commands, aliases, configuration keys,
pagination, `--exec-path=...` and `--list-cmds` selectors are refused.
Terraform and tofu still classify their global options before refusing
`apply` and `destroy`. This filter establishes known inspection dispatch;
arbitrary command arguments, inherited Git configuration and environment, and
the selected executable remain separate trust boundaries. It does not
guarantee that every admitted Git argument is read-only, nor sandbox an
arbitrary executable.

Loading a task validates its bounded plan structure and integrity without
applying new-input defaults or today's command admission policy. Historical
declarations and run argv remain unchanged, including commands now refused for
new work. Starting a task checks every command against current policy; running
a stored check rechecks its argv against current policy before execution or a
new check log or revision. Readable history does not authorize re-execution.

New artifacts sync their file content before no-replace publication, then sync
their descriptor-bound containing directory. New storage components and their
parents are synced through the existing ancestor; starting a task also syncs
the store after publishing its initial record. A reported durability failure
can mean the named artifact is already published: inspect and reload the
absolute task or artifact path in the error before retrying, and keep
published bytes intact rather than replaying an update or starting another
check on that assumption.

### Merging the base branch during a task

A task's plan cannot change after `cw task start`. When the task merges its base
branch, the merge can change packages or module files that no plan check covers;
`cw task add-checks` adds checks that cover them, tied to the merge commit.
Checks can only be added this way, never removed, replaced or narrowed.

1. Fetch the base branch: `git fetch`.
2. Merge it with `git merge --no-ff <base>`, for example
   `git merge --no-ff origin/main`. `--no-ff` records a merge commit even when
   the branch has no commit after the task baseline, which is common mid-task:
   the publication hook gates only `git commit` and `git push`, so it does not
   block the merge, but it denies commits while the record is unverified. For
   the same reason, conclude a conflicted merge with `git merge --continue`,
   not `git commit`. In a non-interactive shell (agents, CI) always use
   `GIT_EDITOR=true git merge --continue`, which keeps the prepared merge
   message; plain `git merge --continue` opens an editor, so use it only in an
   interactive terminal. Where `cw gate install` has run, git's
   `pre-merge-commit` hook lets a merge without conflicts through when every
   commit it takes in is on a remote-tracking branch, as `origin/main` is after
   `git fetch`. It stops any other merge while the record is unverified, such
   as one of a local branch, and `pre-commit` stops `git merge --continue`
   after conflicts ([git hooks](hooks.md#git-hooks)). When git stops a merge,
   the agent stops and asks the user how to go on; without an answer it undoes
   the merge with `git merge --abort`. The agent never sets `CW_GATE=off`
   itself: letting a merge through that way, as in
   `CW_GATE=off git merge --no-ff origin/main`, is the user's decision.
3. Add the checks:

   ```sh
   cw task add-checks --task <task-id> --input amend.json
   ```

   with `amend.json` naming the merge commit (its ID or `HEAD`), the base ref, a
   reason and the checks:

   ```json
   {
     "merge_commit": "HEAD",
     "base_ref": "origin/main",
     "reason": "merged origin/main; go.mod and internal/b changed",
     "checks": [{"id": "widened", "argv": ["go", "test", "./..."], "timeout_seconds": 300}]
   }
   ```

   `timeout_seconds` may be omitted (300). `--input -` reads standard input.
   It prints the task ID, the amendment index, the added check IDs and
   the resolved merge and base commits; `--full` prints the whole record.
4. Run each added check: `cw task run --task <task-id> --check widened`.
5. Run `cw task verify --task <task-id>`.

`cw task add-checks` refuses, exits 1 and writes nothing when:

- the task is `small`, or its evidence rules do not include the Go
  package-test rule (the task started after `cw pack remove go`);
- an added check ID is already a plan or added check, the plan and added checks
  would exceed 256, or an argv is not a direct `go test` that the Go rule
  credits (flags outside the allowlist, such as `-run`, and wrappers such as
  `make` earn no credit); `GOFLAGS` in `cw`'s environment plays no part here;
- an added check's `env_passthrough` breaks the plan's rules for it
  ([what a check runs with](#what-a-check-runs-with));
- the merge does not qualify: `merge_commit` must resolve to a commit with at
  least two parents that is reachable from HEAD and not from the task's
  baseline HEAD or from `base_ref`, and a parent after the first must be
  reachable from `base_ref`. A shallow repository is refused because Git cannot
  answer ancestry across its boundary; `git fetch --unshallow` fixes it;
- an added check, taken in input order, covers no package or module file that
  the merge changed relative to its first parent, that is still changed since
  the task baseline, and that no plan check or earlier added check covers. A
  module file (`go.mod`, `go.sum`, `go.work`, `go.work.sum`) is covered only by
  `./...`.

An accepted amendment is appended to the record's `amendments` (shown by
`cw task show`), removes the stored verification and increments
`evidence_revision`, so a final context prepared before it is stale. Added
checks run, bind and can be cited like plan checks, and the reviewer bundle
lists them after the plan checks; a satisfied requirement must still cite every
plan check it names. Whether a brief with a contract must or must not declare
requirements follows the plan checks only, so an added `-json` check never
changes it. Verify credits only runs stored in the record that passed
at the current source. No credit comes from the base branch's CI, and files the
merge brought in stay in the changed set even when they match the base. Merge
history is inspected only by `add-checks`; later resets or base-branch updates
do not change the added checks.

If the base merge left no merge commit, `add-checks` refuses with a hint naming
`git merge --no-ff`. Recover as follows:

- **Fast-forward.** A plain `git merge` on a branch with no commit after the
  baseline fast-forwards. Check that the branch is not pushed and that
  `ORIG_HEAD` names the commit before the merge, then run
  `git reset --keep ORIG_HEAD` (never `--hard`, which discards uncommitted task
  work) and merge again with `git merge --no-ff <base>`.
- **Squash.** `git merge --squash` stages the squashed changes and leaves HEAD
  unmoved; the hook denies the commit. Undo it with `git reset --merge` only
  when nothing else is staged. Otherwise restore from HEAD only the paths the
  squash staged, `git restore --staged --worktree --source=HEAD -- <those paths>`,
  never other staged work. Then merge with `--no-ff`.
- **Rebase.** After `git pull --rebase` (or with `pull.rebase=true`) on an
  unpushed branch, return to the pre-rebase commit with
  `git reset --keep ORIG_HEAD` and merge with `--no-ff`.

A `cw` release older than `add-checks` cannot read a record carrying
`amendments`, and its hook skips such a record; keep every host that shares a
task store on the same `cw`.

### Cost history

Claude Code deletes transcripts after `cleanupPeriodDays`, and their token and cost
data goes with them. To keep it, append each session to the cost history before
the transcript expires, and read the history back at any time:

```sh
cw metrics import --claude-transcript ~/.claude/projects/<project>/<session-id>.jsonl --append
cw metrics history --since 2026-10-01 --until 2026-10-31 --prices prices.json
```

- **Where.** `--append` writes to `metrics/history.jsonl` under the state
  directory (`--state-dir`, else `$XDG_STATE_HOME/agent-workflows`, else
  `~/.local/state/agent-workflows`), or to `--history FILE`. A new file is created
  with mode 0600 and a missing directory with 0700; an existing file or directory
  keeps its mode, so check it when you point `--history` somewhere shared. A
  symlink or other non-regular file there is refused. The file is limited to
  1 GiB: an append that would pass it fails without writing, and you start a new
  history by moving the old one aside (records in the old file then no longer
  de-duplicate new imports). macOS and Linux only.
- **What.** One JSON line per de-duplicated provider request: `v` (schema version
  1), `kind` (`cw.metrics_request`), `source`, `session_id`, `request_id` (the
  import's de-duplication key), `agent_id`, `agent_role`, `agent_type`, `model`,
  `timestamp` (UTC), `imported_at`, and the token counts (uncached input, 5m, 1h
  and unsplit cache writes, cache reads, output, thinking). It never stores prompt
  or response text, tool inputs or results, file contents, agent descriptions or
  the transcript path. Lines are at most 4096 bytes and text fields at most 256.
- **Idempotent.** A record's identity is `(source, session_id, request_id)`.
  Re-importing a session appends only requests the history does not hold, so
  importing it again, or after it has grown, never double-counts. A stored record
  whose counts differ from the transcript is kept and counted as `conflicting`.
  Requests without a request or message id cannot be de-duplicated and are only
  counted (`unkeyed`). `--append` prints these counts instead of the report.
- **Prices.** The history stores token counts, not dollars. `cw metrics history
  --prices FILE` prices every group with the table you pass, so a later price
  change only needs a different table. The recorded cost-state total of a Claude
  session is not stored; `cw metrics import` still reports it while the
  transcript exists.
- **Concurrency and damage.** Every writer takes an exclusive `flock` on the
  sibling `<history>.lock` file (released if the process dies, never deleted,
  and waited for at most 30 seconds), reads the stored
  identities, and appends whole lines in one write; readers take a shared lock.
  A torn final line from an interrupted write is terminated before the next
  append. `cw metrics history` skips a damaged line, counts it in
  `damaged_lines`, lists the first 100 with their line numbers and reasons, and
  warns on stderr.
- **Report.** `cw metrics history` filters by `--session`, `--model`, and an
  inclusive UTC `--since`/`--until` date range, and prints totals and groups by
  model, UTC day and session. Without `--prices` every amount is unknown, with a
  reason.

#### What an import reports

`import` reads a session read-only and reports each agent (the main session and
every subagent or child rollout) with requests, uncached input, cache writes (5-minute
and 1-hour), cache reads, output and thinking tokens. Claude dollars are the
transcript's recorded `cost-state`, per model and for the session, never allocated to
agents; a model that `cost-state` lists but no transcript entry carries, such as
background calls, is reported as unattributed. Codex rollouts record no cost. `--prices` adds
per-agent estimates from a supplied table
(`{"currency","evidence","models":{"MODEL":{"input_per_million","output_per_million","cache_write_5m_per_million","cache_write_1h_per_million","cache_read_per_million"}}}`),
reported separately from recorded dollars; no price is built in. The report's
`usage` (and `cost`, when recorded) can be copied into an observation; there
`input_tokens` stays total input, and the optional breakdown counters must add up to
it. When a model's transcript and recorded totals differ, for example a `cost-state`
written before the session ended, the report shows the difference and leaves
`usage` and `cost` out.

`--append` (with `--claude-transcript` or `--codex-session`) keeps the counts after
the transcript is deleted. It appends one record per de-duplicated request to the cost history,
`<state>/metrics/history.jsonl` unless `--history` names another file, and prints what
it appended instead of the report. A record holds token counts, IDs, timestamps, the
model and agent labels, and no dollars. Requests already stored are skipped, so
importing again never double-counts. `--append` rejects `--prices`, `--composition`
and `--summary`. `cw metrics history` aggregates the history by model,
UTC day and session, and recomputes dollars from `--prices`.

`--composition` (it needs `--prices`) adds where each agent's input came from. Each
agent is mapped to a role from the `cw task assign` brief whose assignment id its Agent
prompt names, else from its Agent description or agent type; unmapped agents count as
`other`, and the role coverage is reported.
Each request's new input, its total input minus the previous request's, is assigned
to the blocks appended in between: the previous output gets its output tokens
(all of the new input when that is smaller), and the rest is split by byte share across tool output (by tool), `cw`
command output, Skill loads, reference reads, Agent handoffs, user turns and other
context. New input with no visible block is `unrecorded`. The first request is split
into its visible blocks, estimated at the agent's measured bytes per token, and
`system_and_tools`, the remainder. That remainder, the skill listing and the
CLAUDE.md/AGENTS.md instructions form the fixed prefix. Per source the report gives
input tokens, input share, input dollars and re-read cost (tokens × later requests ×
cache-read rate). Input dollars allocate each request's priced input in context order:
cache reads first, then cache writes, then uncached input. It also lists repeated
identical tool calls (same tool and input hash). For an agent that ran
`cw task start`, `cw task consult`, a review `cw task assign` or `cw task verify` (as
commands, including after `if`, `do`, `env`, `nohup`, `sudo` or `timeout`; not text in
a heredoc, a comment or a quoted string, nor history copied into a resumed session), it
gives the stale-phase re-read: earlier-phase blocks, other than the initial context,
re-read in a later phase, beside an estimate for re-sending the initial context at
each boundary. The composition is reported only when the price-table total is within
1% of the recorded `cost-state` and the transcript has no tokens the recorded cost
lacks; otherwise it is withheld with a reason. Codex rollouts record no cost, so
their composition is reported as unsupported. `--summary` prints a short table
instead of JSON.

The agent's rules for recording and comparing observations are in
[measure outcomes](../skills/task-orchestration/references/measure-outcomes.md).

### Usage stores

The session transcript is the source of a session's usage; the cost history is a
copy of its token counts, and a task record keeps usage samples taken from it.
No command reads two of them, so none reconciles them:

| Command | Reads | Writes |
| --- | --- | --- |
| `cw metrics import` | the transcript: a Claude Code session with its subagents, or a Codex rollout with its descendants | nothing |
| `cw metrics import --append` | the transcript, then the request identities in the history | requests the history lacks |
| `cw metrics history` | the cost history only | nothing |
| `cw metrics summarize` | the observation files you name | nothing |
| `cw task usage` | the transcript, then the task record | a usage sample in the task record, when it matters ([cost budget](#cost-budget)) |

When they differ:

- **Transcript and history.** The stored request wins: if the transcript's
  counts for the same identity differ, the history keeps its own and `--append`
  counts the request as `conflicting`; `cw metrics history` reads only the first
  line of an identity. `cw metrics import` reports the transcript as it is now,
  `cw metrics history` what was appended; only the history keeps a session after
  its transcript expires.
- **Within one import.** Recorded dollars come only from the session's last
  cost-state in the main Claude transcript, price-table dollars only from
  `--prices`, and both are reported apart. When transcript tokens and the cost-state differ for a model,
  neither wins: `usage` and `cost` are withheld with `usage_unknown_reason`, and
  an incomplete import also leaves `usage` unknown, never zero. Codex rollouts
  and the history record no dollars, so their only dollars are price-table
  estimates.
- **Task records.** A task record holds the usage samples `cw task usage`
  stored; the task's [budget stop](#budget-stop) reads them, not the cost
  history. A sample is the transcript as it was when sampled, and its dollars
  are estimates from a dated price table, never the cost-state, so they may
  differ from what `cw metrics import` reports for the same transcript. A
  consultation record keeps each adviser call's provider-reported tokens, which
  no command adds to session usage.

## Consultation command

`cw consult` calls the configured advisers: the task's bound set for a task-bound
request, otherwise the providers enabled in the [selected policy](#model-policy)
that you opted in to and did not disable in your preferences, each only with its
key present. A task-bound request is sent only when the task's record holds
its task context exactly as `cw task context` printed it, read from the task
store the hook reads (`$CW_TASK_STORE`, else the state directory's
`workflow-tasks`, [task store](hooks.md#task-store)), and the task was not
abandoned; otherwise `cw consult` exits 1 and writes and sends nothing. For a
task started with `--store DIR`, set `CW_TASK_STORE=DIR`. It works
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
`TYPESAFE_API_KEY`), when you opted in to it, and when it is not disabled in your
preferences. Saving a key with `cw keys set` opts in to its adviser; a key only in
the environment also needs `advisers.enabled` or `--policy`, and until then
`cw doctor` warns "key present, adviser not enabled".

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
both owned by you and not linked. When they are not, `cw task start` and
`cw consult` use no saved key and opt in to no adviser through one: they go on with
the keys in the environment alone and print a warning on stderr that names the
problem. A key in the environment takes precedence over a
saved one. `cw keys status` reports whether each key comes from the environment,
the saved file or neither, without any value or provider call.

To opt in to an adviser whose key is only in the environment, list its provider
ID under `advisers.enabled` in `preferences.json` in the configuration directory;
to disable one and keep its key, list it under `advisers.disabled`, for example
`{"version":1,"advisers":{"enabled":["subq_snap"],"disabled":["openai_decisions"]}}`.
Edit the file by hand; it must be a regular file with mode `0600`, owned by you.
`cw` reads only `advisers.enabled` and `advisers.disabled` from it, refuses an ID
listed in both, and refuses a top-level member other than
`version`, `advisers`, `hosts` and `shared`. An unreadable or
invalid file makes `cw task start` fail before it creates a task. The change applies
to tasks started afterwards: `cw task start` binds the advisers configured at that
moment. A bound adviser whose key is missing at consultation time is recorded as
`not_configured`; `cw consult` still writes the record, exits `5` and names the
variable, and `cw task consult` rejects that record. Supply the key and consult
again. Installation and local validation do not require keys or make hosted
requests.

A terminal export does not update an already running desktop app; use the
[setup instructions](advisers.md#make-keys-visible-to-the-agent) and a fresh
process and session. `cw consult` never reads macOS launchctl or another process's
secrets by itself.

See the [adviser statuses](../skills/task-orchestration/references/adviser-providers.md#statuses)
and the [consultation record contract](../skills/task-orchestration/references/consultation-helper.md)
for offline adjudication and exit codes.
