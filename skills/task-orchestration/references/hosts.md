# Host mapping

What differs between Codex and Claude Code. Identify the host from your own
runtime: under Claude Code, Bash subprocesses have `CLAUDECODE=1`. Project
instructions are `AGENTS.md` under Codex; under Claude Code `CLAUDE.md`, or
`AGENTS.md` when no `CLAUDE.md` applies.

## Profiles and policy

Read [model-policy.yaml](../model-policy.yaml). Under Claude Code,
[model-policy.claude.yaml](../model-policy.claude.yaml) replaces its profiles
and roles (read `sol` and `luna` below as its matching profiles); activation,
advisers and publication stay in `model-policy.yaml`. The policy guides your
dispatch: neither host loads it as settings or changes a running chat's model.

- Before dispatch, resolve each role's profile (model and effort) and scope
  from the current policy. Keep explicit user choices; a user request overrides
  effort only with a value the profile lists in `user_requested_efforts`.
- `sol` at `max` may be configured or explicitly requested; `sol` at `ultra`
  needs an explicit user request the profile allows. Never escalate
  automatically; a saved policy cannot switch an active session.
- Substantive coding, testing, planning, review, diagnosis, documentation, new
  acceptance claims and final verification use the role's configured
  substantive profile, never a `luna` one. Never lower a substantive effort or
  model family.
- Routine execution requires fixed files, revision, verified facts, action
  bounds and authorization from you. New claims, ambiguous scope, conflicts or
  uncertain verification return to substantive ownership. Model selection
  never relaxes a role's action bounds.
- Use a fixed specialist model only within its evidence role.
- Check runtime capabilities before spawning. Pass model and effort
  explicitly; where the host cannot override them, use a compatible inherited
  configuration or report the limit.
- Verify the effective model and effort from host runtime metadata, never
  from worker prose, and disclose a mismatch. Stop an incompatible worker
  before it writes.
- Without an explicit user override, substantive coordination and final
  verification require a compatible main worker. If none is available, retain
  the brief and evidence, name the required fresh-session configuration and
  continue only bounded evidence gathering. A compatible subworker does not
  repair the main worker's mismatch.

## Dispatch under Codex

Create a distinct worker with `fork_turns: "none"` (a full-history fork
rejects overrides) and explicit `model` and `reasoning_effort`. Codex still
injects `$CODEX_HOME/AGENTS.md` and the project `AGENTS.md` into it: keep
task conclusions out of `AGENTS.md`, and report the independence of a Codex
reviewer or test engineer as limited by those files.

## Dispatch under Claude Code

A new Agent call starts with a fresh context.

- Pass `model` and `effort` on every call, built-in agents such as `Explore`
  included; the `model` you pass overrides an agent's frontmatter default.
- Pass the alias of the profile's model family (`opus`, `sonnet` or `haiku`),
  never a full ID. It resolves to the host's current release of that family,
  which can differ from the policy's model ID
  ([host requirements](../../../docs/operations.md#host-requirements)).
- Dispatch every later review of a stage, after that stage's first full
  review, under the `review_followup` role. A saved `review` pin does not
  apply to it; pin it separately.
- Set `subagent_type`:
  - `agent-workflows:spec-writer`, `agent-workflows:architect`,
    `agent-workflows:implementer`, `agent-workflows:test-engineer` and the
    opt-in `agent-workflows:adversarial-tester` for the role of that name;
  - `agent-workflows:reviewer` for `spec_review`, `design_review`,
    `test_review`, `review` and `review_followup`;
  - `agent-workflows:github-worker` for `commit_execution`,
    `pr_authoring_routine` and `pr_submission`;
  - `general-purpose` or a built-in type such as `Explore` for other roles.
- If a type is unavailable, for example when the plugin is not installed,
  dispatch `general-purpose` with the same model, effort and brief and note it
  in the assignment. Check the runtime type, `agent:custom:<name>` in
  transcript metadata (or `/tasks`), with the model and effort.
- Each worker reads its role contract from the path the brief gives, or from
  the plugin root when the brief gives none. A reviewer brief names one of the
  four review contracts.
- These workers have no Skill, Agent or Workflow tool, so only you dispatch.
  When a worker needs a skill, such as `code-review` for a code reviewer, put
  the absolute path of its `SKILL.md` in the brief
  (`<plugin root>/skills/<name>/SKILL.md`; the plugin root is the line
  `Plugin root:` in the task-orchestration `SKILL.md`). For a test engineer,
  list it among the approved inputs.
- `reviewer` and `test-engineer` start without the user, project and local
  CLAUDE.md files; managed policy files still load. Quote in the brief any
  project constraint the work must meet that the contract lacks. The other
  five agents keep CLAUDE.md.

## Test engineer and GitHub worker

Resolve the `test_engineer` role itself: add no host policy role and select
no cheaper publication profile.

`commit_execution`, `pr_authoring_routine` and `pr_submission` use the
`github` profile, with `bounded_execution` scope; the worker's
[contract](github-worker-fallback.md) lists its brief. If that profile's
worker or the metadata check is unavailable, retain the brief and report the
blocker. `fallbacks.github_unavailable: report_blocker`, when the policy has
it, overrides the profile fallbacks for these three roles on both hosts;
without it, follow the policy's bounded-worker fallback. Never silently move
these roles to your own, an expensive or any other model.

## Adviser questions

[Question patterns](question-patterns.md) and `question-patterns.json` are the
same on both hosts. Option names in a request are question content, not model
selections; resolve any worker scope through the current host policy.

## Advisers on either host

Advisers participate only when configured: enabled in `model-policy.yaml`
`consultation.providers`, with a credential present, opted in to
(`advisers.enabled`, a key saved with `cw keys set`, or `--policy`), and not
disabled in the user's preferences (`advisers.disabled` in `preferences.json` in
the configuration
directory; this applies to tasks started afterwards). `cw task start` binds that
set, and both checkpoints use the binding. Advisers are plain HTTPS calls made by
`cw consult`, so they behave the same on both hosts. The coordinator is never one
of the advisers, and a model in `consultation.providers` is an adviser pin
rather than a worker model, so it does not change under Claude Code.
`model-policy.claude.yaml` has no consultation section.

Keys come from the process environment or saved credentials. A Claude Code
session started from the desktop app inherits the app's launch environment, not
your shell profile, so a key exported in a terminal may be missing there. Check
with `cw keys status` before `cw task start`: a missing key at start leaves that adviser
out of the binding, and a bound adviser whose key goes missing later is recorded
as `not_configured`.
