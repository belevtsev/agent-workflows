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

Substantive roles keep Opus 5.5. The three routine GitHub roles use the cheaper
Sonnet 5.5 profile; resolve the current policy before dispatch.

| Profile | Agent tool `model` | Agent tool `effort` |
| --- | --- | --- |
| `substantive` | `opus` | `xhigh` |
| `bounded_execution` | `opus` | `xhigh` |
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
- The settings default stops at `xhigh` because `settings.json` rejects `max`. Use
  `/effort max` for a session when the user asks for it.

## Adviser questions

`references/jev-questions.json` names options such as `luna_lookup`,
`luna_execution` and `sol_judgment`. These identifiers are stable question
content, not model selections. Resolve their scopes through the current host
policy: bounded evidence, bounded execution (including `github` for the three
GitHub roles), or substantive judgment. Both model and effort can differ.

## Advisers on either host

Jev, SubQ Snap and OpenAI Decisions stay required at both checkpoints. They are
plain HTTPS calls made by `cw consult`, so they behave the same on both hosts. The
coordinator is never one of the advisers, and `gpt-6-luna` in
`consultation.providers` is an adviser pin rather than a worker model, so it does
not change under Claude Code.

Keys come from the process environment. A Claude Code session started from the
desktop app inherits the app's launch environment, not your shell profile, so a
key exported in a terminal may be missing there. Check with `cw status`. A missing
key is recorded as unavailable coverage.
