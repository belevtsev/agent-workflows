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

Then read the page for your host:
[Claude Code](hosts-claude-code.md) or [Codex](hosts-codex.md). Contents:
[Profiles and policy](#profiles-and-policy),
[Independent test engineers](#independent-test-engineers-on-either-host),
[Adviser questions](#adviser-questions),
[Advisers on either host](#advisers-on-either-host).

## Profiles and policy

The YAML is configurable dispatch policy, not native host configuration: neither
host loads it as settings or changes a running chat's model. Read
[model-policy.yaml](../model-policy.yaml) and, under Claude Code,
[model-policy.claude.yaml](../model-policy.claude.yaml), which replaces its
profiles and roles (read `sol` and `luna` below as its matching profiles). The
policy files name each profile's model; these pages name profiles and Claude
Code aliases only. The [Codex](hosts-codex.md#profiles) and
[Claude Code](hosts-claude-code.md#dispatching-workers-under-claude-code) give
each host's defaults.

Before dispatch, resolve each role's profile (model and effort) and scope from the
current policy, preserving explicit user choices; a user request can override
effort only with a value the profile lists in `user_requested_efforts`. `sol` at `max`
may be configured or explicitly requested; `sol` at `ultra` needs an explicit user
request the profile allows. Never escalate automatically; a saved policy cannot
switch an active session. Verify model and effort from runtime metadata and
disclose a mismatch. Without an explicit user override, substantive coordination
and final verification require a compatible main worker; if unavailable, retain
the brief and evidence, name the required fresh-session configuration and continue
only bounded evidence gathering. A compatible subworker does not repair the main
worker's mismatch.

Substantive coding, testing, planning, review, diagnosis, documentation, new
acceptance claims and final verification use the role's configured substantive
profile, never a `luna` one. Routine execution requires fixed files, revision, verified facts, action
bounds and authorization from the coordinator; routine GitHub wording can be
drafted from fixed verified facts under the [GitHub worker contract](github-worker-fallback.md).
New claims, ambiguous scope, conflicts or uncertain verification return to
substantive ownership. Model selection never relaxes a role's action bounds.

## Independent test engineers on either host

The [test-engineer contract](contracts/test-engineer.md) applies when a
substantial task authors, changes, removes or disables behavioural tests or their
helpers or data, or when the user asks for an independent author. Resolve the
`test_engineer` role rather than adding a host policy role or selecting a
cheaper publication profile. Preserve configured model and effort overrides and
verify the effective settings.

Dispatch it as your host's page says ([Codex](hosts-codex.md#independent-test-engineers),
[Claude Code](hosts-claude-code.md#independent-test-engineers)). Do not resume an
implementer, coordinator or
source-aware reviewer as the test engineer. Inspect initial injected context and
automatic project instructions as well as the handoff; a fresh conversation alone
does not establish an unexposed context.

Give only approved contract, interface and scaffold inputs and exact test-output
paths; on either host, Go contracts start from the
[declaration extract](contracts/test-engineer.md#extract-go-declarations).
The coordinator retains implementation discovery and raw execution diagnostics.
Inspect available tool transcripts for boundary violations; neither host dispatch
nor native task ownership is a filesystem sandbox. If the test engineer is
exposed, stop or supersede it and use a fresh replacement. Report a blocker when
suitable fresh context or model settings cannot be supplied; do not fall back to
an exposed worker.

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
