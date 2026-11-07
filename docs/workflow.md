# Workflow and local integrations

The global conventions keep user scope, authorization, source evidence, and
verification consistent. Use the relevant skill when its workflow helps the
request. A plan or review remains a plan or review until implementation is
authorized. Publishing, messaging, committing, pushing, and infrastructure
changes need authorization for that action.

For substantial work, the coordinator reads
[`model-policy.yaml`](../skills/task-orchestration/model-policy.yaml), and under
Claude Code also
[`model-policy.claude.yaml`](../skills/task-orchestration/model-policy.claude.yaml),
which replaces its profiles and roles. These are editable routing policies
interpreted by the skill; neither host loads the YAML as native settings or changes
a running chat's model automatically.

Standalone setup can retain release defaults or derive an immutable effective
snapshot from per-field preferences. All host dispatch and native readers use that
same effective policy; explicit values remain pinned while absent fields follow
future releases. Recommended and Balanced are reset operations, and ordinary update
keeps saved choices. No preset grants publication authority or relaxes substantive
role scopes, independent authorship, adviser or review requirements.

The table shows the Codex defaults. Under Claude Code, the `github` profile uses
`claude-sonnet-5-5` at `medium`, and the Claude-only `review_followup` role (every
review after a task's first full review) uses it at `high`; Codex keeps every review
on `review`. Other roles use `claude-opus-5-5` at `high`, except
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
results do not prove correctness or grant permission. Ordinary commit and routine
PR requests explicitly dispatch the [GitHub worker](../skills/task-orchestration/references/github-worker.md),
including small tasks. Pass model and effort explicitly and verify effective host
settings. `fallbacks.github_unavailable: report_blocker`, when present, overrides
the general expensive fallback for these three roles on both hosts; historical
policies without it retain their fallback behavior.

## Independent test authors

Whenever behavioral tests are added or materially changed, including small fixes
and tests-only tasks, the coordinator dispatches a fresh
[test author](../skills/task-orchestration/references/test-author.md) under the
existing `testing` model profile. Helpers, fixtures, golden data, fuzz seeds,
tolerances and removing/disabling tests are part of that ownership. This does not
require new tests for formatting or documentation-only work.

The author gets a versioned requirements/interface packet and approved scaffolding,
not implementation source, history or reasoning. The coordinator or a source-aware
verifier runs integrated tests and gives contract-level results without raw source
diagnostics. Missing behavior or a private test seam returns to the coordinator.
Semantic assertion and fixture changes return to an unexposed author; preserve
the authored patch identity. Implementation exposure requires stopping that
assignment and using a fresh replacement.

Native author assignments use the existing writable role with exact test-file
ownership and no assigned execution checks. A correctly failing regression can
complete authorship while actual passing checks remain separate. Small tasks keep
small native handoffs without substantial checkpoints solely for author dispatch;
substantial changes retain independent source-aware final review. This guidance
does not provide a filesystem sandbox, change model defaults or grant publication
authority. Existing tests are not retroactively claimed as blind-authored.

## Local configuration

`./install.sh` enrolls two settings for each selected host, using its validated
coordinator policy. In Codex `config.toml`:

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

Paths, trusted projects, plugins, MCP settings, permissions, and authentication
remain local decisions. The Go manager preserves unrelated configuration, comments,
and permissions. On each host it owns the two model keys, global instruction block,
and 37 skill registrations, plus one enrolled direct command/PATH registration. It
records original values once and restores them on uninstall. A Claude Code settings
key you have since changed stays yours; see
[operations](operations.md#claude-code-host).

cw is a direct executable link on PATH. It uses recorded installation roots from
any directory; no shell command wrapper is involved. Install selects Bash/zsh for
optional PATH enrollment; --shell none leaves enrollment to the user. Help/version
are offline. Updates and rollbacks apply the selected skill snapshot's model
defaults, while a running session keeps its existing settings.

`cw status` includes `model_profiles`, a map of profile names to `model`,
`reasoning_effort`, and `scope`, read from the validated active snapshot. This
reports configured routing, not live model availability or the current chat's
model. Historical snapshots retain their own profile settings.

The policy remains version 2. Its `sol` profile, `sol_unavailable` fallback and
`max_for_sol` rule keep their names as schema identifiers; the `sol` profile now
runs `gpt-6.1-sol`. `max_for_sol: configured_or_explicit_user_request`
permits a configured Max default when `configured_max_profiles` exactly lists every
Max-default profile. Older `explicit_user_request` policies remain readable and
continue rejecting configured Max for Sol models. The native validator enforces
these rules. New standalone candidates require `cw-manager-v10`, state/journal4
and immutable base/effective receipts. An installation made before v2.0.58, or
migrated by v2.0.58 from one, is refused before any write; run `cw uninstall` with the existing `cw`, then run
install.sh. New records are not promised readable by old managers. Rollback
retains the capable manager and current preferences.

Use compact skill triggers, relevant references, and bounded worker briefs to
control context. Pass exact source revisions, ownership, constraints, and the
required evidence rather than the whole conversation. Effort labels alone do not
prove savings; compare equivalent tasks using quality, elapsed time, and actual
token usage. See [model guidance](https://learn.chatgpt.com/docs/models) and
[manual routing evaluations](skill-evaluation.md).

A later edit to an owned value is a conflict. Malformed TOML and symlinked
configuration are refused. Status, recovery and removal rely on installed
snapshots and the recorded original settings.

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
disabled in your preferences. To disable one and keep its key, clear its "Use at
checkpoints" box in setup's Keys step, or list its provider ID under
`advisers.disabled` in a `--settings` file; tasks started afterwards do not bind it.

| Provider ID | Adviser | Pinned model | Key | Shipped default |
| --- | --- | --- | --- | --- |
| `subq_snap` | SubQ Snap | `subq-snap-preview` | `SUBQ_API_KEY` | Enabled |
| `openai_decisions` | OpenAI Decisions | `gpt-6-luna` | `OPENAI_API_KEY` | Enabled |
| `jev` | TypeSafe Jev | exact `jev-X.Y.Z`, such as `jev-1.14.0` | `TYPESAFE_API_KEY` | Opt-in |

The README's [adviser setup](../README.md#configure-advisers) shows masked terminal
input, presence checks, CLI inheritance and desktop launch environments. An export
in another terminal does not update a running agent or integration-server process;
restart from the configured environment and open a fresh chat. Never commit key
values, put them in prompts, or include them in logs or evidence shared with workers.

`cw status` reports `credential_sources`: for each key, whether its own process has
it in the environment or the private stored file, never the value.
`credential_storage_status` says whether the stored file could be inspected. Keys
are optional for installation, validation and local status. These offline checks do
not establish key validity, service availability, or access from a different process.

### Policy and binding

The shared policy is version 3; any other version is rejected. `consultation` holds
the deadline, attempt and retry settings, the
[question patterns](../skills/task-orchestration/references/question-patterns.json)
path, ownership, reuse, sharing, `comparison: coordinator_evidence_and_verification`,
and `providers`: a possibly empty mapping of enabled provider IDs to pinned models.
Unknown provider IDs, non-string models and moving aliases such as `jev-latest` are
rejected. The shipped default enables `subq_snap` and `openai_decisions`; its
comment shows the `jev: jev-1.14.0` line and the measurement behind leaving Jev out:
across 261 adjudicated consultations, Jev was the only adviser matching the verified
decision in none of 30 splits, was overruled 14 times and was unavailable 23 times,
while Snap matched all 587 resolved questions. The Claude Code policy has no
consultation section; participation follows the shared policy and the keys present.
Policy changes require their own authorized change.

`cw task start` binds the configured set into the task record as `advisers`, and
`cw task context` emits it. A task-bound request consults exactly that set, so later
policy or key changes do not alter a running task's checkpoints. A request without
a task context uses the policy's enabled providers, minus any disabled in the saved
preferences. `cw task start --policy PATH` and `cw consult --policy PATH` default to
the installed release's policy, and a `--policy` copy still honours
`advisers.disabled`; task start fails before creating a task when the policy or the
saved preferences cannot be read or are invalid. To opt
in to Jev, start the task with a private policy copy that adds `jev: jev-1.14.0`,
as the [setup guidance](../README.md#optional-set-up-jev) shows.

### Flow and checkpoints

The task coordinator owns consultations, their scope and reuse. Substantial tasks
follow a fixed flow: classify, before checkpoint, implement and verify, fresh final
checkpoint, and complete only when both checkpoints are resolved. No required state
is skipped. The checkpoints are required when the task has an open decision: real
alternatives with consequential uncertainty. Then `cw task assign` is refused until
the before checkpoint is attached and resolved, and `cw task verify` requires both.
When the approach follows directly from the defect or contract and no changed code
must behave alike on several platforms or shells, the plan declares
`"checkpoints": {"required": false, "reason": "..."}`; verify then lists the
limitation `adviser checkpoints not required: <reason>`, and every other gate
stays. When in doubt, keep them required. For planning or review without
implementation, the before checkpoint precedes substantive drafting or review.
Each checkpoint has a separate request and disposition; before advice cannot
replace the final checkpoint. Reuse is limited to the same checkpoint with
still-applicable evidence, decisions, question meaning and models. Small edits and
direct factual answers keep their bypass.

`cw consult` sends the same permitted minimal text state, questions and Choice
options to every configured adviser concurrently, with one attempt per provider and
a 30-second batch budget covering startup, requests and result transport, and no
automatic retries. Record the source revision and verify excerpts against it before
sending. A participating adviser without its key is `not_configured` and receives
no request; `unavailable` is only for an attempted adviser that failed. With no
adviser attempted, `cw consult` makes no network call and writes a
`no_advisers_configured` record. Records use version 4; earlier versions are
rejected.

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
[setup guidance](../README.md#optional-set-up-jev) explains moving aliases and the
separate image, Box and long-context opt-ins, which the helper never sends.
TypeSafe is hosted in the US, offers zero data retention only to enterprise
customers, and states in its privacy policy that it does not train on prompts.

Before configuring any adviser, confirm that sending source excerpts to that
provider is permitted. The local record retains the prepared request verbatim, so
sanitizing input is the coordinator's responsibility. No credential is written into
snapshots or records.

An optional live availability check requires an explicit request, uses only
synthetic public text, and validates each requested model, expected question IDs,
answer types, allowed options, finite probabilities and distributions. A successful
response is availability evidence for that request, not evidence of calibrated
accuracy or implementation correctness.

### Adjudication

The coordinator owns the final decision. The record holds each adviser's result and
a per-question comparison (`agreement`, `disagreement`, `partial`, `unavailable` or
`no_advisers`), leaving `final_decision` pending. The coordinator compares every
judgment against exact sources and relevant checks, including unanimous ones;
confidence, probabilities, voting and provider identity never select a winner.
`--adjudicate` validates the coordinator's decision offline and writes a new
record. A resolved item needs typed `source:`, `check:` or `observation:` evidence
and verification, never adviser output (check IDs, run IDs and observations cannot
name an adviser or `gpt-6-luna`; `source:` paths are exempt); a reason for each
differing adviser and an investigation note for differing advisers with confidence
of 0.7 or higher, in `partial` as in `disagreement` comparisons; and
`agreement_not_independent: true` when advisers agree on a request whose
`state.untrusted` marks untrusted text. Material missing facts keep a question
unresolved, and an unresolved decision blocks completion. The same rules apply when
no adviser answered. See the
[request and decision contract](../skills/task-orchestration/references/consultation-helper.md).

Unavailable or refusing advisers are reported while source work continues; one
adviser's failure preserves the other results. Installation, validation, CI, status
and the installation verification prompt make no consultation requests. Use the
editable checkout helper for verification; managed snapshot activation remains a
separate authorized action after a clean committed change. Do not edit immutable
installed releases in place.

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
