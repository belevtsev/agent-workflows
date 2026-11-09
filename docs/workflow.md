# Workflow and local integrations

The [global conventions](../global/AGENTS.md) keep user scope, authorization,
source evidence, and verification consistent, and say when a task is substantial.
A substantial task follows the
[task-orchestration](../skills/task-orchestration/SKILL.md) decision flow; each
rule of that flow lives in one of its references, which this page links rather
than repeats. A plan or review remains a plan or review until implementation is
authorized. Publishing, messaging, committing, pushing, and infrastructure
changes need authorization for that action.

For substantial work, the coordinator reads
[`model-policy.yaml`](../skills/task-orchestration/model-policy.yaml), and under
Claude Code also
[`model-policy.claude.yaml`](../skills/task-orchestration/model-policy.claude.yaml),
which replaces its profiles and roles. These are editable routing policies
interpreted by the skill; neither host loads the YAML as native settings or changes
a running chat's model automatically. How the coordinator resolves profiles,
overrides and mismatches is in
[profiles and policy](../skills/task-orchestration/references/hosts.md#profiles-and-policy).

The skill reads the routing from the policy files it ships with; `cw` reads only
the consultation part of a policy (see [model policy](operations.md#model-policy)).
No policy edit grants
publication authority or relaxes substantive role scopes, independent authorship,
adviser or review requirements.

The table shows the Codex defaults. Under Claude Code, the `github` profile uses
`claude-sonnet-5-5` at `medium`, and the Claude-only `review_followup` role (every
later review of a stage, after that stage's first full review) uses it at `high`;
Codex keeps every review on its stage's role. Other roles use `claude-opus-5-5` at `high`, except
the decision roles (planning, architecture and diagnosis), which keep `xhigh`. See the
[host mapping](../skills/task-orchestration/references/hosts.md).

| Work | Default model | Effort |
| --- | --- | --- |
| Coordination and substantive implementation, review, planning, or diagnosis | `gpt-6.1-sol` | `max` |
| Routine commits, PR drafting from verified facts, and submission (`github`) | `gpt-6-luna` | `high` |
| Other bounded execution profiles | `gpt-6-luna` | `xhigh` |
| Bounded read-only lookup or summarization | `gpt-6-luna` | `high` |

The policy preserves fixed specialist settings and describes availability
fallbacks. Sol 6.1 `max` is configured; Sol `ultra` requires an explicit user request.
The coordinator still delegates useful independent assignments and obtains
required independent design challenges. Automatic escalation is disabled. A
worker's evidence does not authorize an external action, and consultation
results do not prove correctness or grant permission. A verified substantial
task is published with `cw publish`, run by the coordinator under the user's
authorization; its fallback cases, or a user request for a bounded worker,
dispatch the [GitHub worker](../skills/task-orchestration/references/github-worker.md); a small
task commits and pushes directly under the user's authorization. Pass model and
effort explicitly and verify effective host settings.
`fallbacks.github_unavailable: report_blocker`, when present, overrides
the general expensive fallback for these three roles on both hosts; historical
policies without it retain their fallback behavior.

## Review loops

Substantial work that builds new behaviour has a review loop at every stage:
spec writer ⇄ reviewer (spec) → architect ⇄ reviewer (design) → implementer ∥
test engineer ⇄ reviewer (tests) → integrate and run → reviewer (code) ⇄
implementer → `cw task verify` → gated commit. One `reviewer` agent runs every
stage with the contract its brief names; findings go back to that stage's
author; the severity grades and the per-stage review cap are in
[review stages](../skills/task-orchestration/references/worker-handoffs.md#review-stages).
The [code review](../skills/task-orchestration/references/contracts/code-review.md)
reads the integrated source against `interface-contract.md` and `design.md`,
with the final check's coverage profile when one exists: it classifies each
unexecuted changed block, and each behaviour finding carries a test that fails
on the current source. The test engineer never sees coverage; an untested
requirement reaches a fresh one only as its requirement ID, contract-level
inputs and expected outcome. Such a reproduction test stays as a regression
test authored with source access: it is not requirement coverage and needs
no test-engineer dispatch.
A small task skips the flow, the spec and design stages included, and the
[contracts overview](../skills/task-orchestration/references/contracts/README.md#flow)
says when a substantial task skips the spec stage. After the before checkpoint,
which is unchanged, advisers join only at four decision points and only when
available
([adviser points](../skills/task-orchestration/references/consultation-helper.md#adviser-points-in-the-flow)).

## Independent test engineers

When a substantial task adds or materially changes behavioral tests, helpers,
fixtures, golden data, fuzz seeds or tolerances, or when the user asks, the
coordinator dispatches a fresh
[test engineer](../skills/task-orchestration/references/contracts/test-engineer.md)
under the `test_engineer` role. For a small task the coder writes the tests and
the reviewer checks each against the Code Review Rules. That contract holds the
input boundary, the Go declaration extract, feedback, replacement and record
rules; the
[host mapping](../skills/task-orchestration/references/hosts.md#independent-test-engineers-on-either-host)
says how to start one on each host, and a [test review](../skills/task-orchestration/references/contracts/test-review.md)
checks its tests before integration.

## Opt-in adversarial tester

A plan opts in at classify, before `cw task start`, when the change touches
concurrency, parsing or untrusted input, security or data integrity, or when the
user asks. A fresh
[adversarial tester](../skills/task-orchestration/references/contracts/adversarial-tester.md)
then reads the interface contract, `design.md`, the integrated source and the
coverage profile, and tries to break the change in parallel with code review.
Every test it keeps fails on the integrated source and cites the requirement or
design rule it shows broken; each is a finding for the implementer and stays as
a regression test written with source access. It never counts as black-box
coverage, and the test engineer never sees it.

## Local configuration

Neither the plugins nor `cw install --host codex` edit a host's model settings.
The coordinator profiles in the policies correspond to these values, which you set
yourself if you want them. In Codex `config.toml`:

```toml
model = "gpt-6.1-sol"
model_reasoning_effort = "max"
```

In Claude Code `settings.json`:

```json
{
  "model": "claude-opus-5-5",
  "effortLevel": "high"
}
```

`cw install --host codex` prints the further recommended Codex settings from
[`global/codex-defaults.toml`](../global/codex-defaults.toml) without editing
`config.toml`. Paths, trusted projects, plugins, MCP settings, permissions, and
authentication remain local decisions. A running session keeps its existing
settings.

The shared policy is version 3. Its `sol` profile, `sol_unavailable` fallback and
`max_for_sol` rule keep their names as schema identifiers; the `sol` profile now
runs `gpt-6.1-sol`. `max_for_sol: configured_or_explicit_user_request`
permits a configured Max default when `configured_max_profiles` exactly lists every
Max-default profile. Older `explicit_user_request` policies remain readable and
continue rejecting configured Max for Sol models. The repository check
(`go test ./internal/skillcheck`) loads the policies.

Use compact skill triggers, relevant references, and bounded worker briefs to
control context. Pass exact source revisions, ownership, constraints, and the
required evidence rather than the whole conversation. Effort labels alone do not
prove savings; compare equivalent tasks using quality, elapsed time, and actual
token usage. See [model guidance](https://learn.chatgpt.com/docs/models) and
[manual routing evaluations](skill-evaluation.md).

## Production planning with OpenSpec

[`production-plan`](../skills/production-plan/SKILL.md) automatically uses
OpenSpec in a scoped project with its own initialized configuration/layout or
declared store. OpenSpec supplies the change's schema, templates, dependencies,
and artifact paths; the skill supplies source-grounded engineering decisions,
failure behavior, acceptance evidence, and required design review. Its focused
[integration reference](../skills/production-plan/references/openspec.md) is loaded
only for that workflow. An unrelated workspace or machine-wide default store
does not opt a project in.

For an existing OpenSpec project, ask:

> Use $production-plan to plan the retry change in this project, continuing
> change `retry-delivery`. Keep our accepted decisions and completed history.

When saving planning artifacts is intended, name that boundary:

> Use $production-plan to update only the planning artifacts for `retry-delivery`.
> Add unchecked follow-up work for changed requirements and stop at the handoff.

Plan Mode always returns a complete chat preview without writing files. An
explicit chat-only request also keeps the plan in chat. Other projects retain
ordinary chat planning. Missing or incompatible OpenSpec tools/configuration
are reported as prerequisites while useful planning continues; the suite does
not install OpenSpec, initialize projects, or change global profiles.

The default schema maps purpose/scope into the proposal, observable behavior
into specifications, engineering decisions/evidence into design, and ordered
implementation/verification into unchecked tasks. Custom schemas and optional
artifacts follow the project's actual instructions. Artifact validation and
completion are distinct from implementation completion and production evidence.
Planning does not automatically apply, sync, archive, commit, or publish a change.

OpenSpec remains an optional project dependency. This adapter's compatibility
baseline is 1.14.1; inspect the installed version and command capabilities for
later releases. CLI inspection disables telemetry and completion hooks per
process without changing user settings. See the [official CLI reference](https://openspec.dev/docs/cli)
and [evaluation boundaries](skill-evaluation.md).

## Decision advisers

Advisers are optional hosted models that return typed Choice judgments at the two
decision checkpoints of a substantial task. They need no separate executable or
SDK: `cw consult` makes plain HTTPS calls. An adviser participates only when it is
configured: enabled in the policy's `consultation.providers` with its pinned model,
with its key present in the process environment or the saved credentials, and not
disabled in your preferences. To disable one and keep its key, list its provider
ID under `advisers.disabled` in `preferences.json` in the configuration directory,
edited by hand; tasks started afterwards do not bind it.

| Provider ID | Adviser | Pinned model | Key | Shipped default |
| --- | --- | --- | --- | --- |
| `subq_snap` | SubQ Snap | `subq-snap-preview` | `SUBQ_API_KEY` | Enabled |
| `openai_decisions` | OpenAI Decisions | `gpt-6-luna` | `OPENAI_API_KEY` | Enabled |
| `jev` | TypeSafe Jev | exact `jev-X.Y.Z`, such as `jev-1.14.0` | `TYPESAFE_API_KEY` | Opt-in |

The [adviser setup](advisers.md#configure-advisers) shows masked terminal
input, presence checks, CLI inheritance and desktop launch environments. An export
in another terminal does not update a running agent or integration-server process;
restart from the configured environment and open a fresh chat. Never commit key
values, put them in prompts, or include them in logs or evidence shared with workers.

`cw keys set NAME` saves a key read from standard input; `cw keys status` reports,
for each key, whether its own process has it in the environment or the private
saved file, never the value. Keys are optional for installation and validation.
These offline checks do not establish key validity, service availability, or access
from a different process.

### Policy and binding

The shared policy is version 3; any other version is rejected. `consultation` holds
the deadline, attempt and retry settings, the
[question patterns](../skills/task-orchestration/references/question-patterns.json)
path, ownership, reuse, sharing, `comparison: coordinator_evidence_and_verification`,
and `providers`: a possibly empty mapping of enabled provider IDs to pinned models.
The Claude Code policy has no consultation section; participation follows the
shared policy and the keys present. Policy changes require their own authorized
change. Which advisers participate, how `cw task start` binds them, what
`--policy` does and how to opt in to Jev are in
[participation](../skills/task-orchestration/references/consultation-helper.md#participation);
the measurement behind leaving Jev out is in
[why Jev is opt-in](../skills/task-orchestration/references/adviser-providers.md#why-jev-is-opt-in).

### Flow and checkpoints

The task coordinator owns consultations, their scope and reuse. Substantial tasks
follow the [decision flow](../skills/task-orchestration/SKILL.md#follow-the-decision-flow):
classify, before checkpoint, implement and verify, fresh final checkpoint, and
complete only after `cw task verify`. When the checkpoints are required, how each
request is prepared, sent, recorded and reused is in
[task checkpoints](../skills/task-orchestration/references/consultation-helper.md#task-checkpoints)
and the sections after it; the plan shape that declares them not required is in
[start and bind a task](../skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task).
After the before checkpoint, advisers join only at the four
[adviser points](../skills/task-orchestration/references/consultation-helper.md#adviser-points-in-the-flow),
and only when the task's binding holds one; otherwise each point is skipped and
recorded as "no advisers configured", and the flow runs unchanged.

### Provider notes

**SubQ Snap.** Read SubQ's [Decisions API](https://docs.subq.ai/endpoints/decisions-api/)
and [models](https://docs.subq.ai/using-the-api/models/). `subq-snap-preview` is the
documented public preview alias, not an immutable version pin; records keep the
requested and returned models. SubQ [retains data indefinitely by default](https://docs.subq.ai/using-the-api/data-retention/).
Organization-level zero data retention is available and applies to Decisions. The
documented per-request `store: false` opt-out applies to Chat Completions and
Responses; this helper does not claim it for Decisions.

**OpenAI Decisions.** The adviser uses `gpt-6-luna` through the dedicated
[Decisions API](https://developers.openai.com/api/docs/guides/decisions), separately
from dispatching a Luna worker profile. The adapter sends deterministic text for the
same shared state, maps questions to named Choice arrays, and normalizes named
answers and probability arrays. A question refusal is missing coverage; valid
choices from the same response remain usable. Read the current
[API contract](https://developers.openai.com/api/reference/resources/decisions/methods/create)
and applicable [data controls](https://developers.openai.com/api/docs/guides/your-data)
before sharing.

**TypeSafe Jev (opt-in).** `skills-manifest.json` registers `typesafe-ai` from
[`third_party/typesafe-ai`](../third_party/typesafe-ai/SKILL.md); its
[development-consultation guide](../third_party/typesafe-ai/references/development-consultations.md)
and the official [API contract](https://docs.typesafe.ai/api) and
[model reference](https://docs.typesafe.ai/models) describe the service. The
[Jev 1.13 limitations](https://docs.typesafe.ai/model-jaggedness/jev-1.13) are
version-specific; do not describe them as verified 1.14 behavior. The
[setup guidance](advisers.md#optional-set-up-jev) explains moving aliases and the
separate image, Box and long-context opt-ins, which the helper never sends.
TypeSafe is hosted in the US, offers zero data retention only to enterprise
customers, and states in its privacy policy that it does not train on prompts.

Before configuring any adviser, confirm that sending source excerpts to that
provider is permitted. The local record retains the prepared request verbatim, so
sanitizing input is the coordinator's responsibility. No credential is written into
records.

An optional live availability check requires an explicit request, uses only
synthetic public text, and validates each requested model, expected question IDs,
answer types, allowed options, finite probabilities and distributions. A successful
response is availability evidence for that request, not evidence of calibrated
accuracy or implementation correctness.

### Adjudication

The coordinator owns the final decision: the record holds each adviser's result
and a per-question comparison and leaves `final_decision` pending, and
`cw consult --adjudicate` validates the coordinator's decision offline. The
decision shape and every rule it enforces are in
[adjudicate without network access](../skills/task-orchestration/references/consultation-helper.md#adjudicate-without-network-access).

Unavailable or refusing advisers are reported while source work continues; one
adviser's failure preserves the other results. Installation, validation, CI and
`cw doctor` make no consultation requests.

## Connected services

Authenticate Jira, Confluence, and GitHub independently in each host on each device
using the intended account for each service. Skill installation does not
establish connector access or copy authentication. Verify the identity and a
permitted read for each service separately before using it for a task.
The public repository can be cloned without GitHub authentication. Cloning does
not verify a connector's identity, private-repository access, or write access.

Use a task's explicit authorization before writing to any service. After an
authorized write, read it back; if the result is uncertain, reconcile remotely
before retrying. Keep company content, project history, credentials, private
audits, and private evaluation inputs outside this reusable source repository.
Maintained evaluation fixtures contain synthetic public examples only.
