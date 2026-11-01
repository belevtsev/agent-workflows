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

The table shows the Codex defaults. Under Claude Code, every role uses
`claude-opus-5-5`, at `xhigh` for substantive and bounded execution work and `high`
for bounded evidence. See the
[host mapping](../skills/task-orchestration/references/hosts.md).

| Work | Default model | Effort |
| --- | --- | --- |
| Coordination and substantive implementation, review, planning, or diagnosis | `gpt-6.1-sol` | `max` |
| Bounded routine execution | `gpt-6-luna` | `xhigh` |
| Bounded read-only lookup or summarization | `gpt-6-luna` | `high` |

The policy preserves fixed specialist settings and describes availability
fallbacks. Sol `max` is configured; Sol `ultra` requires an explicit user request.
The coordinator still delegates useful independent assignments and obtains
required independent design challenges. Automatic escalation is disabled. A
worker's evidence does not authorize an external action, and consultation
results do not prove correctness or grant permission.

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
  "effortLevel": "xhigh"
}
```

Paths, trusted projects, plugins, MCP settings, permissions, and authentication
remain local decisions. The Go manager preserves unrelated configuration, comments,
and permissions. On each host it owns the two model keys, global instruction block,
and 26 skill registrations, plus one enrolled direct command/PATH registration. It
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

The policy remains version 2. `max_for_sol: configured_or_explicit_user_request`
permits configured Sol Max when `configured_max_profiles` exactly lists every
Max-default profile. Older `explicit_user_request` policies remain readable and
continue rejecting configured Sol Max. The native validator and optional Python
helper enforce the same rules. Manager v3/v4/v5 needs the one-time v6 bootstrap
migration before it can validate the expanded manifest and personal adoption.

Use compact skill triggers, relevant references, and bounded worker briefs to
control context. Pass exact source revisions, ownership, constraints, and the
required evidence rather than the whole conversation. Effort labels alone do not
prove savings; compare equivalent tasks using quality, elapsed time, and actual
token usage. See [model guidance](https://learn.chatgpt.com/docs/models) and
[manual routing evaluations](skill-evaluation.md).

A later edit to an owned value is a conflict. Malformed TOML and symlinked
configuration are refused. Legacy installation records and original settings are
preserved during migration; status/recovery/removal rely on installed snapshots.

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

## TypeSafe, Jev, and SubQ Snap

`skills-manifest.json` registers `typesafe-ai` from
[`third_party/typesafe-ai`](../third_party/typesafe-ai/SKILL.md). Resolve the installed
skill through the host's skill catalog and read its
[development-consultation guide](../third_party/typesafe-ai/references/development-consultations.md)
relative to that skill's root. Jev is TypeSafe's hosted System One model. The
task-orchestration [consultation helper](../skills/task-orchestration/references/consultation-helper.md)
requires Jev, SubQ Snap and OpenAI Decisions at equal standing; these advisers need no
separate executable or SDK.
The existing Jev-only helper remains supported unchanged.

Use the official [quick start](https://docs.typesafe.ai/introduction/quickstart)
and [Console API keys page](https://console.typesafe.ai/keys) to obtain a key.
Use the [SubQ authentication guide](https://docs.subq.ai/using-the-api/authentication/)
and [API Console](https://console.subq.ai/) for its separate key. The README's
[Jev](../README.md#set-up-jev), [SubQ Snap](../README.md#set-up-subq-snap) and
[OpenAI Decisions](../README.md#set-up-openai-decisions) setup
steps show masked terminal input, boolean presence checks, CLI inheritance, and
desktop launch environments. Supply `TYPESAFE_API_KEY`, `SUBQ_API_KEY` and `OPENAI_API_KEY` through
the process environment or a local secret manager; an export in another terminal
does not update a running agent or integration-server process. Restart from the
configured environment and open a fresh chat. Never commit key values, put them
in prompts, or include them in logs or evidence shared with workers.

`cw status` reports offline booleans `jev_credential_present` and
`subq_credential_present`, plus `openai_credential_present`, for its own process,
never credential values. Credentials
are optional for installation, validation, and local status. These booleans do not
establish key validity, service availability, or access from a different process.

The task coordinator owns consultations, their scope, and reuse. Every substantial
task requires all three advisers before implementation and again during final
review of the completed work and current check results, before completion or
authorized publication. For planning or review without implementation, the first
checkpoint precedes substantive drafting or review. Each checkpoint has a separate
request and disposition; pre-implementation advice cannot replace final review.
Reuse is limited to the same checkpoint with still-applicable evidence, decisions,
question meaning, and models. Record restricted sharing or unavailable providers
at each checkpoint and follow the source-evidence fallback without adding retries.
Small edits and direct factual answers retain their bypass.

Checkpoint timing is distributed agent guidance in the global instructions,
orchestration entrypoint, and policy comments. The helper validates individual
requests and records; it does not schedule or enforce task phases. Distributing
phase instructions is separate from the manager's policy validation contract.
The installed policy keeps version 2 and `consultation.model`, requires providers
`jev: jev-1.14.0`, `subq_snap: subq-snap-preview` and
`openai_decisions: gpt-6-luna`, and retains
`comparison: coordinator_evidence_and_verification`. Historical policies keep
`codex_evidence_and_verification`, which stays valid.
Send the same permitted minimal text state, questions, and Choice options to
participating advisers concurrently, with one attempt per provider and a 30-second
batch budget covering startup, requests, and result transport.
Each worker uses the remaining budget; cleanup is bounded afterward, with no automatic retries.
Record source revision and verify excerpts against that revision before sending.
Read the current
[API contract](https://docs.typesafe.ai/api),
[model reference](https://docs.typesafe.ai/models), and the selected model's
available limitations before consulting. The linked
[Jev 1.13 limitations](https://docs.typesafe.ai/model-jaggedness/jev-1.13) are
version-specific; do not describe them as verified 1.14 behavior. The
[setup guidance](../README.md#set-up-jev) explains moving aliases and the separate
image, Box and long-context opt-ins. A newer alias or upstream example does not
change the installed pin.
Policy upgrades require their own authorized change. Read SubQ's
[Decisions API](https://docs.subq.ai/endpoints/decisions-api/) and
[models](https://docs.subq.ai/using-the-api/models/) as well. `subq-snap-preview`
is the documented public preview alias, not an immutable version pin; record
requested and returned models and refresh affected consultations after changes.
The common workflow is Choice-only and fits participating advisers' limits; Snap's larger
context and image support do not expand the shared request.

### OpenAI Decisions

The required adviser uses `gpt-6-luna` through the dedicated
[Decisions API](https://developers.openai.com/api/docs/guides/decisions), separately
from dispatching a Luna worker profile. Follow the README's
[OpenAI setup](../README.md#set-up-openai-decisions) and supply
`OPENAI_API_KEY` through the process environment. All three advisers are required
at both consultation checkpoints within permitted sharing. A missing key records
unavailable coverage, never a disabled optional adviser. `--without-openai` is
rejected for this policy. A service failure or refusal preserves the other results
and is disclosed as incomplete consultation coverage.

The adapter sends deterministic text for the same shared state, maps questions to
named Choice arrays, and normalizes named answers and probability arrays. An OpenAI
question refusal is missing advisory coverage; valid choices from the same response
remain usable. Unavailability and refusals stay distinct. Required-three records
use version three and reject a disabled required provider. The helper still
adjudicates historical version-one pair and version-two optional records offline
while preserving the originals.

Read the current [API contract](https://developers.openai.com/api/reference/resources/decisions/methods/create)
and applicable [data controls](https://developers.openai.com/api/docs/guides/your-data)
before sharing. The common contract is text and Choice only. Provider-specific limits
and failures preserve the other advisers' results. No OpenAI SDK or additional system
package is installed, and no credential is written into snapshots or records.

SubQ [retains data indefinitely by default](https://docs.subq.ai/using-the-api/data-retention/).
Organization-level zero data retention is available and applies to Decisions.
The documented per-request `store: false` opt-out applies to Chat Completions and
Responses; this Decisions helper does not claim it. Confirm permitted sharing and
retention before sending source evidence. The local record retains the prepared
request verbatim, so sanitizing input is the coordinator's responsibility.

An optional live availability check requires an explicit request, uses only
synthetic public text, and validates each requested model, expected question IDs,
answer types, allowed options, finite probabilities, and distributions. Follow
the installed helper and guide rather than adding retries or sending private
source material for a setup check. A successful response is availability
evidence for that request, not evidence of calibrated accuracy or implementation
correctness.

The coordinator owns the final decision. The consultation helper records independent provider
results and per-question agreement, disagreement, partial coverage, or unavailable
coverage, leaving `final_decision` pending. The coordinator compares the judgments against
exact sources and relevant tests or observations, including when all advisers agree.
Neither confidence, probabilities, voting, nor provider identity selects a winner.
Record the conclusion, evidence references, independent verification, rejected
judgments and reasons, unresolved facts, and provider coverage in the agreed task
record. The helper's `--adjudicate` mode validates that disposition and writes a new
final record without network calls. Resolved items require evidence, verification,
and reasons for rejecting successful providers' differing choices; material missing
facts keep the final decision unresolved. See the
[request and decision contract](../skills/task-orchestration/references/consultation-helper.md).

Missing or inconclusive providers are reported while source work continues. One
provider's failure preserves the other results and is disclosed as missing coverage.
Installation, validation, CI, status, and the installation verification prompt make
no consultation requests. Use the editable checkout helper for verification; managed
snapshot activation remains a separate authorized action after a clean committed
change. Do not edit immutable installed releases in place.

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
