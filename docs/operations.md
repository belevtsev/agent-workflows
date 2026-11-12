# Operations

Claude Code gets the skills and worker agents from the plugin in
`.claude-plugin/`, and Codex from `.codex-plugin/` and
`.agents/plugins/marketplace.json`. This page covers installing them and `cw`,
the Codex instruction block, paths, the model policy lookup and adviser keys.

## Install

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
- **Trust.** Codex runs neither plugin hook until you trust them, and `codex exec`
  does not say it skipped them. Per Codex's documentation, trust is recorded
  against the hook's hash, so trust them again when an update changes
  `hooks/hooks.json`.
- **Plugin options.** Codex has no plugin configuration and sets no
  `CLAUDE_PLUGIN_OPTION_*` variable; the hooks read the environment Codex was
  started with. Unset, the gate and the prompt for unrecorded changes are on
  and the doctor summary is off. A recorded run turned the gate off with
  `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false`; the other two variables use the
  same lookup but were not run. No option affects `cw publish`.
- **Evidence.** The plugin rows come from runs recorded with codex-cli
  0.167.2 ([Codex hooks](hooks.md#codex), [recorded host runs](hooks.md#recorded-host-runs));
  the last column lists what they did not cover. The session-start context
  is added again on each resume and after each compaction.

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
one passed check (see [evidence rules](../skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task)).

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

There is no state file, journal, snapshot or rollback. Both commands reject
`--dry-run`. `cw install --host claude` and `cw uninstall --host claude` are usage
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
an alias of `cw pack`; absent means the default selection). `--dry-run` is a
global flag; commands that would write without a preview reject it with exit
status 2: `cw install`, `cw uninstall`, `cw gate install|uninstall`,
`cw keys set|unset`, `cw graphify build`, the `cw task` state commands and
`cw metrics import --append`.

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
| 0 | ok | The command did what was asked. |
| 1 | failure | `cw` could not do it: invalid input, an I/O error, a damaged or locked record. A check that passed but whose log or test results `cw` could not write (`log_write_failed`, `test_results_write_failed`) is recorded as failed and exits 1. |
| 2 | usage | The command line is wrong: an unknown flag, a missing or conflicting option (such as a `cw task` command without `--task`), or `--dry-run` where it is rejected. Nothing ran. |
| 3 | denied | The gate blocks the operation: `cw publish` `denied`; `cw hook git` blocking a commit or push. |
| 4 | refused | A precondition or the record's state refuses the operation: `cw publish` `refused`; `cw gate install` leaving a hook it did not write unchanged, and `cw gate install` or `uninstall` refusing a hooks directory that can serve other repositories (without `--shared`) or, for install, a missing one outside the git directory; every `cw graphify` `unavailable` report, whatever its `code`; a `cw task` command on a record from a newer `cw` (upgrade `cw`), or on one without `packs`, `test_rules` or `baseline_manifest`, or that binds `go.package-test/v1` (start a new task). Other records `cw` cannot read, such as version 1 or 2, exit 1. |
| 5 | negative result | The command worked and the answer is no: `cw task verify` incomplete; `cw task run` with a check that did not pass (failed, timed out, changed the source, or left a process behind) or that `cw` would not run with an unverified `go` ([which go](#which-go-a-check-runs)); `cw consult` with partial or unavailable coverage, an unresolved adjudication or a bound adviser without its key. |
| 6 | unknown | `cw publish` cannot establish whether a step happened. |
| 7 | mismatch | `cw publish` read back a step that differs from the request. |
| 130 | cancelled | The command was interrupted. |

On 5, `cw task verify` and `cw task run` still print the verification or the
check run on stdout, which names the reason. `cw consult` prints its status
line, or with `--summary` the one-line view, and writes the record to the
`--output` path. The task commands other
than `verify` and `run` still exit 1 when the record's state refuses them, for
example an abandoned task or a `cw task add-checks` refusal. The plugin hooks
(`cw hook pre-publish` and `cw hook session-start`) answer in JSON and exit 0
([hooks](hooks.md)). `cw hook git` exits 3 when the gate blocks the commit,
merge commit or push, which stops git ([git hooks](hooks.md#git-hooks)). `cw gate install` exits 4
when a hook it did not write is in the way, and still writes the other hooks;
`cw gate install` and `uninstall` exit 4 without changing anything when the
hooks directory can serve other repositories and `--shared` is not given
([installing the git hooks](hooks.md#installing-the-git-hooks)).

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
calls advisers. `--summary` prints one line with only the checks that are missing,
degraded or stale.

`cw task` keeps source-bound requirements, actual argv check execution, references,
the adviser binding fixed at `cw task start`, two substantial-task decision
checkpoints (unless the plan declares them not required, with a reason that
verify lists) and the coordinator's decisions in a private
external task directory, by default `STATE/workflow-tasks`. Every task command's
`--task` takes the directory `start` printed or the task's ID (32 hex digits), which
names that directory in the task store: `$CW_TASK_STORE` when set (as for a task
started with `--store DIR`), else `STATE/workflow-tasks`; write `./ID` for a directory of that name. Its `start`, `assign`,
`handoff`, `accept`, `assess`, `consult`, `run`, `verify` and `trace` commands accept `--summary` for a
one-line result instead of the whole record (`run`: the whole check run, plus failing
test names, from a `go test -json` run's per-test results when it recorded them, and first error lines when it failed; `verify`: without its fixed
limitations), `cw doctor` and `cw consult` accept it too, `cw task show
--field <dotted.path>` prints only selected values, `cw task trace` reports, without
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
test in a latest run at the current source makes that test `failed`. See the [global command guide](../skills/task-orchestration/references/global-workflow.md).

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

`cw task timeline --task DIR [--summary]` reports the wall time a task spent, from
timestamps the record already stores, and is read-only: it loads the record with
the same validation as `cw task show` and writes nothing, so `--dry-run` changes
nothing. It prints one JSON object with `task`, `task_id`, `started_at` (the
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
duration computed from one; a missing value is never shown as `0s`. `--summary`
prints JSON Lines rather than a one-line result: one compact object for the task
(`task_id`, `started_at`, `updated_at`, `elapsed`), then one per entry with its
start under `start` and its durations, omitting `task`, `submitted_at`,
`accepted_at` and `finished_at`.

`cw publish` commits a verified task's files, pushes the commit and, with
`--pr create` or `--pr edit`, creates or edits its pull request. It runs only
after `cw task verify` passed on the current source and no other open task record
gates the repository, commits whole files of the task's verified change set, or
of the part of it that `--path` names, reads every step back, and continues from
the observed state when the same command runs again. Once its checks pass, it
appends a `started` entry (`publication-NNNNNN.json`) to the task directory, and
a `finished` entry when it ends. `--summary` prints one line.
Its exit statuses (0 published, 1 failed, 2 usage, 3 denied, 4 refused,
6 unknown, 7 mismatch, 130 cancelled) and what to do for each are in the
[publication reference](../skills/task-orchestration/references/github-worker.md),
which also names the cases that go to the GitHub worker instead. It needs Git
2.24 or newer.

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
- With `core.autocrlf` set to `input` or true, a commit-set file that contains
  a CR byte, binaries included, refuses the publication as 4
  `content-conversion`.
- A Git lock that a concurrent git process holds for more than about two seconds
  reads as a stale lock (4 `git-locked`, or 6 `commit-unknown` after a failed
  commit), so check that no git process runs before removing one.
- A remote with a push URL for another repository, several URLs or an
  `ssh_config` host alias in only one URL is refused as 4 `remote-split`, and
  with a pull request any host alias is refused as 4 `repo-mismatch`.
- A remote that Git hands to a remote helper is refused as 4
  `remote-split`, since the helper picks where the push goes: a
  `<transport>::<address>` URL, a scheme other than `https`, `http`, `ssh`,
  `git+ssh`, `ssh+git`, `git` or `file`, in lower case, or a remote with
  `remote.<name>.vcs` set. `host:path`, `[user@host:port]:path` and local
  paths are accepted.

A task check cannot run `cw publish`: `cw task start` refuses such a plan. This
is a guard, not a sandbox, since a check can still run another program that
publishes.

The shared instructions apply across projects. A first-party Go repository, a
Terraform repository and a Roblox project retain their own checks and operational
rules. Native task commands execute only explicitly selected argv; repository text,
measurement input and model advice do not authorize publication or deployment.
`cw publish` is the only command that writes to a remote, and only after verify
passed. The coordinator runs it under the user's authorization; cw never
authorizes a publication, and no flag or text in it stands for the user's
consent.

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
GOHOSTARCH CGO_ENABLED GOEXPERIMENT GOWORK GOAMD64 GOARM64 CC CXX`, with the
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
  hash of each eligible file) when the run started, the same digest a verified
  record stores as `verification.source_sha256`. Committing, staging or
  switching branches over the same bytes leaves it unchanged. The task
  directory keeps that manifest as `manifest-<source_sha256>.json`, written
  once per digest. A worker result records the same member for the source it
  was submitted at. When that manifest would exceed 8 MiB, the run or result
  is recorded without `source_sha256` and no manifest is kept.
- `env_key`: an HMAC-SHA256 of what the run's environment selects: for a go
  check, every value its `go env` read printed; for any check, each variable
  its `env_passthrough` names, set to its value or unset. Its key is a random
  32-byte secret of the task, kept in the task directory as `run-key-secret`
  (mode 0600) from the first run. No value is stored, and without that file a
  printed `env_key` does not confirm a guessed value, even a weak password.
  Within one task, two runs share a key exactly when those values are equal;
  keys of different tasks are not comparable.

Runs and results recorded before these members existed, or without them,
lack them, so they are never reused. No rule reads the keys yet: they are the identity that reuse of a
passed run will compare.

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
   cw task add-checks --task <task-id> --input amend.json --summary
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
   `--summary` prints the task ID, the amendment index, the added check IDs and
   the resolved merge and base commits.
4. Run each added check: `cw task run --task <task-id> --check widened`.
5. Run `cw task verify --task <task-id> --summary`.

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

### Usage stores

The session transcript is the source of a session's usage; the cost history is a
copy of its token counts. No command reads both, so none reconciles them:

| Command | Reads | Writes |
| --- | --- | --- |
| `cw metrics import` | the transcript: a Claude Code session with its subagents, or a Codex rollout with its descendants | nothing |
| `cw metrics import --append` | the transcript, then the request identities in the history | requests the history lacks |
| `cw metrics history` | the cost history only | nothing |
| `cw metrics summarize` | the observation files you name | nothing |

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
- **Task records.** A task record holds no session usage. A consultation record
  keeps each adviser call's provider-reported tokens, which no command adds to
  session usage.

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

See the [consultation record contract](../skills/task-orchestration/references/consultation-helper.md)
for adviser statuses, offline adjudication and exit codes.
