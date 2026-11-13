# Adviser providers

Read this when you configure, change or evaluate an adviser provider, or need a
provider's data terms. Running a checkpoint needs only the
[consultation helper](consultation-helper.md).

## Why Jev is opt-in

The shipped policy (version 3) enables SubQ Snap and OpenAI Decisions; the policy
comment shows the line that adds Jev. Across 261 adjudicated consultations, Jev
was the only adviser matching the verified decision in none of 30 splits, was
overruled 14 times and was unavailable 23 times. Snap matched all 587 resolved
questions, which suggests deference rather than independent verification.

## Provider contracts

Read each configured provider's current contracts before first sharing evidence
with it, once per provider and policy version rather than at every checkpoint.
Record what you read in `adviser-terms-reviewed.md` in the agent-workflows state
directory (default `~/.local/state/agent-workflows/`), one entry per provider:

```markdown
- subq_snap | policy version 3 | 2026-11-06 | https://docs.subq.ai/endpoints/decisions-api/, https://docs.subq.ai/using-the-api/models/, https://docs.subq.ai/using-the-api/data-retention/
```

Reuse that record until the policy version or the set of configured providers
changes, or 30 days pass; then read the pages again and update it. The contracts:

- SubQ: [Decisions API](https://docs.subq.ai/endpoints/decisions-api/),
  [models](https://docs.subq.ai/using-the-api/models/) and
  [retention policy](https://docs.subq.ai/using-the-api/data-retention/).
- OpenAI: [guide](https://developers.openai.com/api/docs/guides/decisions),
  [API contract](https://developers.openai.com/api/reference/resources/decisions/methods/create)
  and [data controls](https://developers.openai.com/api/docs/guides/your-data).
- Jev, when enabled: the installed `typesafe-ai` entrypoint and development
  consultation guide.

## Data handling

Before configuring any adviser, confirm that sending source excerpts to that
provider is permitted for the code and organization involved.

- **SubQ:** SubQ [retains data indefinitely by default](https://docs.subq.ai/using-the-api/data-retention/).
  Organization-level zero data retention is available and applies to Decisions. Its
  documented per-request `store: false` setting covers Chat Completions and
  Responses; the helper does not claim that setting for Decisions. Credentials are
  described in the [SubQ authentication guide](https://docs.subq.ai/using-the-api/authentication/).
- **TypeSafe (Jev):** TypeSafe is hosted in the US. Zero data retention is available
  only to enterprise customers. Its privacy policy states that it does not train on
  prompts.
- **OpenAI:** keep request content within the account's
  [data controls](https://developers.openai.com/api/docs/guides/your-data).


## Jev version and capability boundaries

When Jev is enabled, its policy entry must be an exact version, which keeps a run
independent of moving aliases. As checked on 16 October 2026, TypeSafe's
[model reference](https://docs.typesafe.ai/models) maps `jev-latest` to `jev-1.13.0`
and `jev-preview` to `jev-1.14.0`; consult that reference for subsequent changes.
The record retains the returned version, and a response that differs from the
requested pin is rejected. The
[1.13 jaggedness notes](https://docs.typesafe.ai/model-jaggedness/jev-1.13) apply to
1.13 and do not establish 1.14 limitations or improvements.

The [1.14 announcement](https://typesafe.ai/blog/jev-1-14) introduces experimental
[images](https://docs.typesafe.ai/images) and
[Box](https://docs.typesafe.ai/primitives/box) answers. They require
`TypeSafe-Danger-Zone-Images: true`. Extended context requires the separate
`TypeSafe-Danger-Zone-Long-Context: true` header; it raises the standard 64k request
and 32k per-question token limits to 262k for both. This helper sends neither
header, accepts no image assets and remains Choice-only for all advisers.

## OpenAI Decisions

The fixed endpoint is `POST https://api.openai.com/v1/decisions`. The adapter maps
canonical shared state to deterministic JSON text in `input`, question IDs to
`name`, and each criterion key/description to a `choices` value/description. It
normalizes named answer arrays and probability arrays into the common record. The
request keeps the shared evidence fingerprint; endpoint-specific encoding does not
give an adviser different facts or question meanings.

Read the current [guide](https://developers.openai.com/api/docs/guides/decisions),
[create contract](https://developers.openai.com/api/reference/resources/decisions/methods/create)
and applicable [data controls](https://developers.openai.com/api/docs/guides/your-data).
OpenAI currently accepts at most 200 questions; an unsupported OpenAI request is
recorded as `unavailable` without breaking the other advisers' results. OpenAI's
predicate, score, image and other input features do not expand this shared contract.

OpenAI can refuse an individual question while answering others. The record keeps
those choices and marks the refused question as missing coverage; a refusal never
becomes a negative choice. A mixed response is `partial`; an all-refused response
is `refusal`.

## Multilingual evaluation

`cwdev eval multilingual` provides a small synthetic baseline for the configured
advisers. It is separate from task consultations, skill-discovery evaluations and
the repository checks. The corpus lives in `evals/multilingual` of the source checkout,
not in the installed skill; `cwdev` is a maintainer tool that reads it from that
checkout, selected with `--source` (by default the current directory).

The twelve neutral case IDs cover durable acknowledgement, tenant isolation,
cancellation and release identity. Each family has supporting, contradictory and
missing-evidence observations. English, Ukrainian and German variants preserve the
facts, claim, instructions and option meanings. The stable choices are `supported`,
`contradicted` and `insufficient_evidence`. The request builder uses only raw
inputs. The separate oracle's per-case expected choice and evaluator rationale never
enter a provider request.

Run offline validation without network access, credential lookup or output files:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual
```

Run it from a source checkout. A hosted run requires `--live --output-dir PATH`
and a new private directory outside source repositories:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual --live --output-dir ~/.codex/workflow-audits/multilingual-baseline-01
```

Only configured providers run. The report lists every known provider; one that is
not configured is `not_run` with reason `not_configured`, which is not a failure.
With no configured provider, a live run makes no network call, reports every
provider as not run and exits `0`.

Freeze the reviewed input and oracle hashes before the first hosted request. Each
language forms one batch of twelve questions. The configured providers receive
identical shared input concurrently; language batches run sequentially, so each
configured provider gets at most three requests. The run uses the existing
30-second batch budget and never retries.

The private `report.json`, `summary.md` and safe per-language consultation records
retain input/oracle/policy/runner provenance, runtime identity, requested and
returned models, measured latency and token usage. Scores include correct, incorrect
and missing counts per provider/language and overall. Accuracy uses valid answers as
its denominator; coverage uses all expected answers. Also inspect correct/total and
complete language-triplet consistency. Zero-denominator metrics remain unknown.
Consistency can include consistently incorrect choices and is not accuracy.

A valid `insufficient_evidence` choice is scored against the oracle normally.
Refusals, malformed responses and unavailable providers remain missing coverage.
Incorrect choices do not fail execution: a fully answered run exits `0`, incomplete
coverage from a configured provider exits `2`, invalid inputs or output errors exit
`1`, and an interruption exits `130` while preserving completed batches. Existing
run directories are never overwritten. Credentials and raw transport exceptions are
excluded from reports.

The scorer does not choose an adviser or fill a task's `final_decision`. A fixture
defect invalidates affected results; preserve the original run and correct the
corpus as a separate version rather than changing labels to match answers. A small
synthetic baseline does not establish calibrated confidence, production quality or
a version improvement. Offline tests exercise the runner with fake transports;
installation, source validation and native CI never invoke the hosted evaluation.
