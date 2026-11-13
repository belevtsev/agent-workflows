# Claude Code host

Read with the shared [host mapping](hosts.md). Contents:
[Dispatching workers](#dispatching-workers-under-claude-code),
[Worker agent types](#worker-agent-types),
[Independent test engineers](#independent-test-engineers).

## Dispatching workers under Claude Code

Substantive roles keep `opus` at `high`. The decision roles (`planning`,
`architecture` and `diagnosis`) keep `xhigh` through the `decision` profile. The
Claude-only `review_followup` role (every later review of a stage, after that
stage's first full review), `summarization` and the three routine GitHub roles
use cheaper `sonnet` profiles, and `lookup` uses `haiku`; resolve the
current policy before dispatch. A saved `review` pin does not apply to
`review_followup`; pin it separately.

| Profile | Agent tool `model` | Agent tool `effort` |
| --- | --- | --- |
| `substantive` | `opus` | `high` |
| `decision` | `opus` | `xhigh` |
| `bounded_execution` | `opus` | `high` |
| `review_followup` | `sonnet` | `high` |
| `github` | `sonnet` | `medium` |
| `bounded_evidence` | `sonnet` | `high` |
| `lookup` | `haiku` | `high` |

- Pass `model` and `effort` on every dispatch, including built-in agents such as
  `Explore`, which otherwise use their own default model. The plugin agents below
  pin a default in their frontmatter, but the Agent tool's `model` overrides it,
  so the policy's role (for example `review_followup` on `sonnet`) still applies.
- Agent accepts model aliases, not full IDs: pass the alias of the profile's
  model family (`opus`, `sonnet` or `haiku`). An alias resolves to the host's
  current release of that family, which can differ from the policy's model ID.
  Per-invocation `effort` needs the Claude Code version in the
  [host requirements](../../../docs/operations.md#host-requirements).
- `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` forces `CLAUDE_CODE_SUBAGENT_MODEL`, or the
  inherited model if it is unset, instead of the Agent `model` argument.
  `CLAUDE_CODE_EFFORT_LEVEL` overrides per-invocation effort. Organization
  `availableModels` restrictions can also substitute the inherited model. See
  [Claude Code subagent dispatch](https://code.claude.com/docs/en/sub-agents).
- Check the actual model and effort in host runtime metadata, such as `/tasks` or
  transcript metadata, rather than worker prose. Stop an incompatible worker
  before writes. If the configured GitHub worker or metadata verification is
  unavailable, report the blocker; do not silently use an expensive model. The
  [GitHub worker contract](github-worker-fallback.md#brief-and-dispatch) covers the shared fallback precedence.
- Standard subagents start with a fresh context. This matches the Codex rule of
  dispatching with no forked history.
- In a non-interactive `claude -p` session, dispatch workers in the foreground
  (without `run_in_background`) and wait for their results. The process exits when
  the coordinator's turn ends, and a background worker and its work are lost
  with it.
- The coordinator profile's effort is `high` by default; `settings.json`
  `effortLevel` is the user's to set. `settings.json` rejects `max`; use
  `/effort max` or `/effort xhigh` for a session when the user asks for it.

### Worker agent types

Seven Claude agent definitions ship in the `agent-workflows` plugin's `agents/`
directory. They have no Skill tool, so the worker starts without the skill
listing, and no Agent or
Workflow tool, so only the coordinator dispatches. Dispatch these roles
with the namespaced `subagent_type`:

| Role | `subagent_type` | Frontmatter default |
| --- | --- | --- |
| `spec_writer` | `agent-workflows:spec-writer` | `opus`, `high` |
| `architect` | `agent-workflows:architect` | `opus`, `xhigh` |
| `implementer` | `agent-workflows:implementer` | `opus`, `high` |
| `test_engineer` | `agent-workflows:test-engineer` | `opus`, `high` |
| `adversarial_tester` (opt-in) | `agent-workflows:adversarial-tester` | `opus`, `high` |
| `spec_review`, `design_review`, `test_review`, `review`, `review_followup` | `agent-workflows:reviewer` | `opus`, `high` |
| `commit_execution`, `pr_authoring_routine`, `pr_submission` | `agent-workflows:github-worker` | `sonnet`, `medium` |

- Each reads its [role contract](contracts/README.md) from the path the brief
  gives, or from the plugin root when the brief gives none; a reviewer brief
  names one of the four review contracts.
- `reviewer` and `test-engineer` set `omitClaudeMd: true`: they start without the user, project and local CLAUDE.md files, so
  author priors and coordinator instructions stay out of an independent review
  or test. Managed policy files still load. Their contracts carry the project
  rules they apply, and the brief quotes any other project constraint the work
  must meet. The other five agents keep CLAUDE.md, because they work to the
  project's conventions.
- That isolation holds on Claude Code only; [Codex](hosts-codex.md#worker-isolation)
  has no per-agent switch.
- Other roles keep `general-purpose` or a built-in type such as `Explore`.
- These workers cannot load skills by name. When a worker needs one, such as
  `code-review` for a code reviewer, put the absolute path of its `SKILL.md` in
  the brief (`<plugin root>/skills/<name>/SKILL.md`, where the plugin root is the
  line `Plugin root:` in the task-orchestration `SKILL.md`); for a test engineer,
  list it among the approved inputs.
- If the type is unavailable, for example when the plugin is not installed,
  dispatch `general-purpose` with the same model, effort and brief and
  note it in the assignment. Only the token cost differs.
- The runtime agent type appears in transcript metadata as
  `agent:custom:<name>`; check it with the model and effort.

## Independent test engineers

Use a fresh `agent-workflows:test-engineer`
Agent invocation with the resolved model alias and explicit `effort`, accounting
for the overrides above.
