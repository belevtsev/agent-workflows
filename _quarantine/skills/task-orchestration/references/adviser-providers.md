# Adviser guidance

Read this only when the task binds advisers: `cw task show --field advisers` is
not empty. With an empty binding the [checkpoint helper](consultation-helper.md)
is enough, and the flow runs as if advisers did not exist. Users configure
advisers ([setup](../../../docs/advisers.md#configure-advisers)); never opt one
in for them.

Contents:

- [Before the first consultation](#before-the-first-consultation)
- [Adviser points in the flow](#adviser-points-in-the-flow)
- [Participation](#participation)
- [Statuses](#statuses)
- [Data handling](#data-handling)
- [Provider notes](#provider-notes)

## Before the first consultation

Read each bound provider's current contracts before first sharing evidence with
it. If `adviser-terms-reviewed.md` in the state directory (default
`~/.local/state/agent-workflows/`) is missing, older than 30 days, or names
another policy version or provider set, read the pages again and update it, one
entry per provider:

```markdown
- subq_snap | policy version 3 | 2026-11-06 | https://docs.subq.ai/endpoints/decisions-api/, https://docs.subq.ai/using-the-api/models/, https://docs.subq.ai/using-the-api/data-retention/
```

- SubQ: [Decisions API](https://docs.subq.ai/endpoints/decisions-api/),
  [models](https://docs.subq.ai/using-the-api/models/) and
  [retention policy](https://docs.subq.ai/using-the-api/data-retention/).
- OpenAI: [guide](https://developers.openai.com/api/docs/guides/decisions),
  [API contract](https://developers.openai.com/api/reference/resources/decisions/methods/create)
  and [data controls](https://developers.openai.com/api/docs/guides/your-data).
- Jev, when enabled: the installed `typesafe-ai` entrypoint and development
  consultation guide.

## Adviser points in the flow

Once the task has started, and after the before checkpoint when its checkpoints
are required, advisers enter the [flow](contracts/README.md#flow) at four
decision points only, never at every step:

| Point | When | Record |
| --- | --- | --- |
| a | After the spec review, on each open question the spec leaves to a choice | interim consultation |
| b | On the architect's choice between real options, before the design review | interim consultation |
| c | Before the coordinator rejects a `blocker` or `major` finding at any [review stage](worker-handoffs.md#review-stages) | interim consultation |
| d | The final checkpoint before `cw task verify` | the task's `final` checkpoint |

With any adviser bound, consult at each point. An adviser that fails or refuses
is recorded `unavailable` or `refusal` and blocks nothing; disclose it, and do
not retry.

An interim consultation (points a to c) is an ad hoc request: follow the
[quick path](consultation-helper.md#quick-path) without `cw task context`
(the before context matches only the baseline source), pass the `--policy` the
task started with, if any, and stop after `cw consult --adjudicate`: there is no
phase to attach it to. Consult only when the binding is not empty. Keep its files
with the task's other private records, outside source, and cite its path and
outcome in the ledger, or for point c in the finding's disposition `reason`.
Point a's decisions reach the spec writer as the coordinator's answers, and point
b's reach the architect, never the adviser output itself.

Inside a substantial task these points replace the suggestions in specialist
skills (such as `code-review`, `test-strategy`, `software-architecture`,
`production-plan`, `principal-engineer` and `security-threat-model`) to consult
advisers on a consequential judgment: such a judgment waits for the next point,
or is point c when it rejects a finding. Outside a substantial task those skills
keep their own rule.

## Participation

The task's binding is the participation set of every task-bound request; `cw`
checks the embedded context against the task record and refuses an edited,
missing or stale one. An ad hoc request uses the selected policy's enabled
providers that the user opted in to and did not disable; the others are recorded
`not_configured` and never contacted. Ad hoc participation therefore matches the
binding unless the policy, preferences or keys changed since `cw task start`.
Without `--policy PATH`, `cw` uses `$CW_MODEL_POLICY`, then
`~/.agents/skills/task-orchestration/model-policy.yaml` when present, then the
policy embedded in `cw`.

| Provider ID | Pinned model | Credential |
| --- | --- | --- |
| `jev` | exact `jev-<major>.<minor>.<patch>`, such as `jev-1.14.0` | `TYPESAFE_API_KEY` |
| `subq_snap` | `subq-snap-preview` | `SUBQ_API_KEY` |
| `openai_decisions` | `gpt-6-luna` | `OPENAI_API_KEY` |

The shipped policy (version 3) enables `subq_snap` and `openai_decisions`; Jev is
opt-in ([why](#why-jev-is-opt-in)). An OpenAI Decisions call does not dispatch a
`luna` worker or inherit its reasoning effort.

### Opt in to Jev

Only when the user asks: copy `skills/task-orchestration/model-policy.yaml` to a
private location, add `jev: jev-1.14.0` under `consultation.providers` (replacing
`{}` if it reads `providers: {}`), set `TYPESAFE_API_KEY`, and start the task
with `cw task start --policy /path/to/copy.yaml`. Its checkpoints then follow the
binding; pass the copy to an ad hoc `cw consult --policy`. A copy still honours
`advisers.disabled` in the saved preferences.

## Statuses

Each participating provider ends with one status:

| Status | Meaning |
| --- | --- |
| `success` | Valid answers to every question |
| `partial` | OpenAI answered some questions and refused others |
| `refusal` | OpenAI refused every question |
| `unavailable` | Attempted and failed: `timeout`, `http_error` (with `http_status`), `network_error`, `invalid_response`, `worker_error` or, for OpenAI, `unsupported_request` |
| `not_configured` | Participating, but no credential; no request was made |

A real call makes one attempt per provider within a 30-second batch budget, with
no retry or substitute model; one provider's failure keeps the others' results.
A bound provider that is `not_configured` makes `cw consult` exit `5` and name
the variable: ask the user to set it, then consult again.

Each question's comparison is `agreement` when every attempted provider chose
the same option, `disagreement` when every one chose and the choices differ,
`partial` otherwise, `unavailable` when there is no choice, and `no_advisers`
when nothing was attempted. These states describe coverage and choice equality,
not truth: investigate a disagreement, and verify an agreement as closely as any
other answer.

## Data handling

Before the first consultation, confirm that sending source excerpts to each
bound provider is permitted for the code and organization involved.

- **SubQ:** SubQ [retains data indefinitely by default](https://docs.subq.ai/using-the-api/data-retention/).
  Organization-level zero data retention is available and applies to Decisions.
  Its per-request `store: false` setting covers Chat Completions and Responses;
  the helper does not claim it for Decisions.
- **TypeSafe (Jev):** hosted in the US; zero data retention only for enterprise
  customers; its privacy policy states that it does not train on prompts.
- **OpenAI:** keep request content within the account's
  [data controls](https://developers.openai.com/api/docs/guides/your-data).

## Provider notes

### Why Jev is opt-in

Across 261 adjudicated consultations, Jev was the only adviser matching the
verified decision in none of 30 splits, was overruled 14 times and was
unavailable 23 times. Snap matched all 587 resolved questions, which suggests
deference rather than independent verification.

### Jev versions

The policy entry must be an exact version; a returned version that differs from
the pin is rejected. TypeSafe's [model reference](https://docs.typesafe.ai/models)
maps its moving aliases. The
[1.13 jaggedness notes](https://docs.typesafe.ai/model-jaggedness/jev-1.13) do not
establish 1.14 limitations. The helper sends neither the images nor the
long-context header that 1.14 offers, and stays Choice-only for every adviser.

### OpenAI Decisions

The adapter maps the shared state to deterministic JSON text, question IDs to
`name` and criteria to `choices`, so OpenAI receives the same facts and question
meanings as the other advisers. OpenAI accepts at most 200 questions; an
unsupported request is recorded `unavailable` without affecting the other
advisers. A refused question is missing coverage, never a negative choice.

The [multilingual evaluation](../../../docs/advisers.md#evaluate-multilingual-judgments)
of the configured advisers is a maintainer tool, separate from task consultations.
