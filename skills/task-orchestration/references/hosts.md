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

## Dispatching workers under Claude Code

Substantive roles keep Opus 5.5 at `high`. The decision roles (`planning`,
`architecture` and `diagnosis`) keep `xhigh` through the `decision` profile. The
three routine GitHub roles use the cheaper Sonnet 5.5 profile; resolve the current
policy before dispatch.

| Profile | Agent tool `model` | Agent tool `effort` |
| --- | --- | --- |
| `substantive` | `opus` | `high` |
| `decision` | `opus` | `xhigh` |
| `bounded_execution` | `opus` | `high` |
| `github` | `sonnet` | `medium` |
| `bounded_evidence` | `opus` | `high` |

- Pass `model` and `effort` on every dispatch, including built-in agents such as
  `Explore`, which otherwise use their own default model.
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
- The installer writes the coordinator profile's effort, `high` by default, as
  `effortLevel`. `settings.json` rejects `max`; use `/effort max` or `/effort xhigh`
  for a session when the user asks for it.

## Independent test authors on either host

The [test-author contract](test-author.md) applies whenever behavioral tests or
their helpers/data are authored, changed, removed or disabled. `test_author` is
the dispatch identity; resolve the existing `testing` profile rather than adding
a host policy role or selecting a cheaper publication profile. Preserve configured
model/effort overrides and verify the effective settings.

On Codex, create a distinct worker with `fork_turns: "none"` and explicit `model`
and `reasoning_effort`. On Claude, use a fresh Agent invocation with the resolved
model alias and explicit `effort`, accounting for the overrides above. Do not
resume a coder, coordinator or source-aware reviewer as the author. Inspect initial
injected context and automatic project instructions as well as the handoff; a
fresh conversation alone does not establish an unexposed context.

Give only approved contract/interface/scaffold inputs and exact test-output paths.
The coordinator retains implementation discovery and raw execution diagnostics.
Inspect available tool transcripts for boundary violations; neither host dispatch
nor native task ownership is a filesystem sandbox. If the author is exposed, stop
or supersede it and use a fresh replacement. Report a blocker when suitable fresh
context/model settings cannot be supplied; do not fall back to an exposed worker.

## Adviser questions

`references/question-patterns.json` holds provider-neutral authoring rules and
generic examples, the same on both hosts. Option names in a request are question
content, not model selections; resolve any worker scope through the current host
policy. Both model and effort can differ.

## Advisers on either host

Advisers participate only when configured: enabled in `model-policy.yaml`
`consultation.providers` and with a credential present. `cw task start` binds that
set, and both checkpoints use the binding. Advisers are plain HTTPS calls made by
`cw consult`, so they behave the same on both hosts. The coordinator is never one
of the advisers, and `gpt-6-luna` in `consultation.providers` is an adviser pin
rather than a worker model, so it does not change under Claude Code.
`model-policy.claude.yaml` has no consultation section.

Keys come from the process environment or saved credentials. A Claude Code
session started from the desktop app inherits the app's launch environment, not
your shell profile, so a key exported in a terminal may be missing there. Check
with `cw status` before `cw task start`: a missing key at start leaves that adviser
out of the binding, and a bound adviser whose key goes missing later is recorded
as `not_configured`.
