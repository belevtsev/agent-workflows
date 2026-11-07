# Host mapping

The workflow runs under Codex or Claude Code. Consultation, handoff, review, and
publication rules are the same on both. This reference covers what differs. In
these skills, "the coordinator" means the primary agent on whichever host is
running.

Identify the host from your own runtime. Under Claude Code, Bash subprocesses
have `CLAUDECODE=1`.

| Concern | Codex | Claude Code |
| --- | --- | --- |
| Profiles and roles | `model-policy.yaml` | `model-policy.claude.yaml` |
| Activation, advisers, publication | `model-policy.yaml` | `model-policy.yaml` |
| Global instructions | `$CODEX_HOME/AGENTS.md` | `CLAUDE.md` in the Claude config directory |
| Personal skills | `~/.agents/skills` | `skills/` in the Claude config directory |
| Explicit skill invocation | `$skill-name` | `/skill-name` |
| Project instructions | `AGENTS.md` | `CLAUDE.md`, or `AGENTS.md` when no `CLAUDE.md` applies |

## Profiles and policy

The YAML is configurable dispatch policy, not native host configuration: neither
host loads it as settings or changes a running chat's model. Read
[model-policy.yaml](../model-policy.yaml) and, under Claude Code,
[model-policy.claude.yaml](../model-policy.claude.yaml), which replaces its
profiles and roles (read Sol and Luna below as its matching profiles). Codex
defaults: Sol 6.1 `max` substantive, Luna `high` bounded evidence and `github`,
Luna `xhigh` other bounded execution; Claude `github`: Sonnet 5.5 `medium`.

Before dispatch, resolve each role's profile (model and effort) and scope from the
current policy, preserving explicit user choices; a user request can override
effort only with a value the profile lists in `user_requested_efforts`. Sol `max`
may be configured or explicitly requested; Sol `ultra` needs an explicit user
request the profile allows. Never escalate automatically; a saved policy cannot
switch an active session. Verify model and effort from runtime metadata and
disclose a mismatch. Without an explicit user override, substantive coordination
and final verification require a compatible main worker; if unavailable, retain
the brief and evidence, name the required fresh-session configuration and continue
only bounded evidence gathering. A compatible subworker does not repair the main
worker's mismatch.

Substantive coding, testing, planning, review, diagnosis, documentation, new
acceptance claims and final verification use the configured non-Luna substantive
profile. Routine execution requires fixed files, revision, verified facts, action
bounds and authorization from the coordinator; routine GitHub wording can be
drafted from fixed verified facts under the [GitHub worker contract](github-worker.md).
New claims, ambiguous scope, conflicts or uncertain verification return to
substantive ownership. Model selection never relaxes a role's action bounds.

## Dispatching workers under Claude Code

Substantive roles keep Opus 5.5 at `high`. The decision roles (`planning`,
`architecture` and `diagnosis`) keep `xhigh` through the `decision` profile. The
Claude-only `review_followup` role (every review after a task's first full review)
and the three routine GitHub roles use cheaper Sonnet 5.5 profiles; resolve the
current policy before dispatch. A saved `review` pin does not apply to
`review_followup`; pin it separately.

| Profile | Agent tool `model` | Agent tool `effort` |
| --- | --- | --- |
| `substantive` | `opus` | `high` |
| `decision` | `opus` | `xhigh` |
| `bounded_execution` | `opus` | `high` |
| `review_followup` | `sonnet` | `high` |
| `github` | `sonnet` | `medium` |
| `bounded_evidence` | `opus` | `high` |

- Pass `model` and `effort` on every dispatch, including built-in agents such as
  `Explore`, which otherwise use their own default model, and the cw worker agents
  below, whose definitions set neither.
- Agent accepts model aliases, not full IDs: `claude-opus-5-5` maps to `opus`,
  and `claude-sonnet-5-5` to `sonnet`. An alias can resolve to another release.
  Per-invocation `effort` requires Claude Code 2.1.292 or newer.
- `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` forces `CLAUDE_CODE_SUBAGENT_MODEL`, or the
  inherited model if it is unset, instead of the Agent `model` argument.
  `CLAUDE_CODE_EFFORT_LEVEL` overrides per-invocation effort. Organization
  `availableModels` restrictions can also substitute the inherited model. See
  [Claude Code subagent dispatch](https://code.claude.com/docs/en/sub-agents).
- Check the actual model and effort in host runtime metadata, such as `/tasks` or
  transcript metadata, rather than worker prose. Stop an incompatible worker
  before writes. If the configured GitHub worker or metadata verification is
  unavailable, report the blocker; do not silently use an expensive model. The
  [GitHub worker contract](github-worker.md) covers the shared fallback precedence.
- Standard subagents start with a fresh context. This matches the Codex rule of
  dispatching with no forked history.
- In a non-interactive `claude -p` session, dispatch workers in the foreground
  (without `run_in_background`) and wait for their results. The process exits when
  the coordinator's turn ends, and a background worker is lost with it: in one
  measured replay that discarded a 36-request review.
- The coordinator profile's effort is `high` by default; `settings.json`
  `effortLevel` is the user's to set. `settings.json` rejects `max`; use
  `/effort max` or `/effort xhigh` for a session when the user asks for it.

### Worker agent types

Six Claude agent definitions ship with the `agent-workflows` plugin. They have no
Skill tool, so the worker starts without the skill listing (about 6K fewer input
tokens on its first request). Dispatch these roles with the namespaced
`subagent_type`:

| Role | `subagent_type` |
| --- | --- |
| `review`, `review_followup` | `agent-workflows:cw-reviewer` |
| `test_author` (the `testing` profile) | `agent-workflows:cw-test-author` |
| `commit_execution`, `pr_authoring_routine`, `pr_submission` | `agent-workflows:cw-github-worker` |
| `architect` | `agent-workflows:architect` |
| `implementer` | `agent-workflows:implementer` |
| `test_engineer` | `agent-workflows:test-engineer` |

- The last three ship in the plugin's `agents/` directory. Each reads its
  [role contract](contracts/README.md) from the path the brief gives, or from the
  plugin root when the brief gives none.
- Other roles keep `general-purpose` or a built-in type such as `Explore`.
- These workers cannot load skills by name. When a worker needs one, such as
  `code-review` for a reviewer, put the absolute path of its `SKILL.md` in the brief
  (`<plugin root>/skills/<name>/SKILL.md`, where the plugin root is the line
  `Plugin root:` in the task-orchestration `SKILL.md`); for a test author, list it
  among the approved inputs.
- If the type is unavailable, for example when the plugin is not installed,
  dispatch `general-purpose` with the same model, effort and brief and
  note it in the assignment. Only the token cost differs.
- The runtime agent type appears in transcript metadata as
  `agent:custom:<name>`; check it with the model and effort.

## Independent test authors on either host

The [test-author contract](test-author.md) applies when a substantial task
authors, changes, removes or disables behavioral tests or their helpers/data, or
when the user asks for an independent author. `test_author` is
the dispatch identity; resolve the existing `testing` profile rather than adding
a host policy role or selecting a cheaper publication profile. In the five-role
flow the [test engineer](contracts/test-engineer.md) is that identity and resolves
the `test_engineer` role instead. Preserve configured model/effort overrides and
verify the effective settings.

On Codex, create a distinct worker with `fork_turns: "none"` and explicit `model`
and `reasoning_effort`. On Claude, use a fresh `agent-workflows:cw-test-author` Agent invocation
with the resolved model alias and explicit `effort`, accounting for the overrides
above. Do not resume a coder, coordinator or source-aware reviewer as the author.
Inspect initial injected context and automatic project instructions as well as the
handoff; a fresh conversation alone does not establish an unexposed context.

Give only approved contract/interface/scaffold inputs and exact test-output paths;
on either host, Go contracts start from the
[declaration extract](test-author.md#extract-go-declarations).
The coordinator retains implementation discovery and raw execution diagnostics.
Inspect available tool transcripts for boundary violations; neither host dispatch
nor native task ownership is a filesystem sandbox. If the author is exposed, stop
or supersede it and use a fresh replacement. Report a blocker when suitable fresh
context/model settings cannot be supplied; do not fall back to an exposed worker.

## Adviser questions

[Question patterns](question-patterns.md) and `question-patterns.json` are the
same on both hosts. Option names in a request are question content, not model
selections; resolve any worker scope through the current host policy.

## Advisers on either host

Advisers participate only when configured: enabled in `model-policy.yaml`
`consultation.providers`, with a credential present, and not disabled in the user's
preferences (`advisers.disabled` in `preferences.json` in the configuration
directory; this applies to tasks started afterwards). `cw task start` binds that
set, and both checkpoints use the binding. Advisers are plain HTTPS calls made by
`cw consult`, so they behave the same on both hosts. The coordinator is never one
of the advisers, and `gpt-6-luna` in `consultation.providers` is an adviser pin
rather than a worker model, so it does not change under Claude Code.
`model-policy.claude.yaml` has no consultation section.

Keys come from the process environment or saved credentials. A Claude Code
session started from the desktop app inherits the app's launch environment, not
your shell profile, so a key exported in a terminal may be missing there. Check
with `cw keys status` before `cw task start`: a missing key at start leaves that adviser
out of the binding, and a bound adviser whose key goes missing later is recorded
as `not_configured`.
