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

Every role uses Opus 5.5. Effort is what separates bounded work from substantive
work.

| Profile | Agent tool `model` | Agent tool `effort` |
| --- | --- | --- |
| `substantive` | `opus` | `xhigh` |
| `bounded_execution` | `opus` | `xhigh` |
| `bounded_evidence` | `opus` | `high` |

- Pass `model` and `effort` on every dispatch, including built-in agents such as
  `Explore`, which otherwise use their own default model.
- The `model` field accepts aliases, not full model IDs, so the policy's
  `claude-opus-5-5` becomes `opus`. The alias follows Anthropic's current Opus
  release while the coordinator's default stays pinned. When a task depends on the
  exact model, check what the worker reports and record any difference as a
  limitation.
- `CLAUDE_CODE_EFFORT_LEVEL` overrides per-dispatch effort, so report the effective
  effort if that variable is set.
- Standard subagents start with a fresh context. This matches the Codex rule of
  dispatching with no forked history.
- The settings default stops at `xhigh` because `settings.json` rejects `max`. Use
  `/effort max` for a session when the user asks for it.

## Adviser questions

`references/jev-questions.json` names options such as `luna_lookup`,
`luna_execution` and `sol_judgment`. These identifiers are stable question
content, not model selections. Under Claude Code, read them as the
`bounded_evidence`, `bounded_execution` and `substantive` profiles. They choose an
effort level and an ownership boundary, since all three use the same model.

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
