# Advisers

Advisers are optional. This page covers which hosted advisers exist, how to set
up their keys, how `cw consult` uses them at the decision points of the flow,
and how the coordinator decides. Policy details are in
[workflow and integrations](workflow.md#decision-advisers).

## Configure advisers

Advisers are optional hosted models that return typed Choice judgments at the
decision points of the flow. An adviser participates only when it is configured:

- it is enabled in `consultation.providers` of the active
  [model policy](../skills/task-orchestration/model-policy.yaml) with its pinned model;
- its key is present in the process environment or the saved credentials;
- you opted in to it: you saved its key with `cw keys set`, listed its provider
  ID under `advisers.enabled` in your preferences, or passed `--policy`, which
  opts in to every provider that policy enables;
- it is not disabled in your preferences.

Save a key with `cw keys set NAME`, which also opts in to its adviser, or export
it; a key in the environment takes precedence over a saved one
([adviser keys](operations.md#adviser-keys)). An exported key alone binds no
adviser, and `cw doctor` warns "key present, adviser not enabled". To use it,
list the provider ID under `advisers.enabled` in `preferences.json` in the
configuration directory, such as
`{"version":1,"advisers":{"enabled":["openai_decisions"]}}`. To turn
an adviser off and keep its key, list its provider ID under `advisers.disabled`
in `preferences.json` in the configuration directory, such as
`{"version":1,"advisers":{"disabled":["openai_decisions"]}}`. The change applies
to tasks started afterwards.

The shipped policy (version 3) enables SubQ Snap and OpenAI Decisions. Jev is
opt-in. An enabled adviser without its key receives no request and is recorded
as `not_configured`, distinct from `unavailable` (attempted and failed) and
`refusal`. `cw task start` binds the advisers configured at that moment into the
task record, and later policy or key changes do not alter that task's
checkpoints; a bound adviser whose key goes missing must be consulted again with
its key before the checkpoint can be attached. With no adviser configured,
`cw consult` writes a `no_advisers_configured` record without network access, and
the coordinator adjudicates each checkpoint on source evidence under the same rules.

Before configuring any adviser, confirm that sending source excerpts to that
provider is permitted for the code and organization involved. Each provider's
data handling is summarized in its section below and in the
[helper contract](../skills/task-orchestration/references/adviser-providers.md#data-handling).
Installation and offline validation work without keys. Never paste a key into
chat or commit it.

### Set up SubQ Snap

Obtain a key from the official [SubQ API Console](https://console.subq.ai/)
using its [authentication guide](https://docs.subq.ai/using-the-api/authentication/).
Store it outside the source tree in your local secret manager. For a temporary
terminal session, enter it with the masked prompt for your shell and keep tracing
disabled.

Bash:

```bash
set +x
IFS= read -r -s -p 'SubQ API key: ' SUBQ_API_KEY
printf '\n'
export SUBQ_API_KEY
```

zsh:

```zsh
set +x
read -r -s 'SUBQ_API_KEY?SubQ API key: '
printf '\n'
export SUBQ_API_KEY
```

Snap's documented model is [`subq-snap-preview`](https://docs.subq.ai/using-the-api/models/),
a public preview alias. Records retain the requested and returned model IDs.
SubQ [retains request data indefinitely by default](https://docs.subq.ai/using-the-api/data-retention/);
organization-level zero data retention is available for Decisions.

Then opt in: `cw keys set SUBQ_API_KEY --from-env SUBQ_API_KEY` saves the key and
opts in, or list `subq_snap` under `advisers.enabled` to use the exported key alone.

### Set up OpenAI Decisions

Obtain an API key from the [OpenAI Platform](https://platform.openai.com/api-keys)
and store it in your local secret manager. The adviser uses
[`gpt-6-luna` through the Decisions API](https://developers.openai.com/api/docs/guides/decisions).
This API call is separate from dispatching a Luna worker with a reasoning profile.

Bash:

```bash
set +x
IFS= read -r -s -p 'OpenAI API key: ' OPENAI_API_KEY
printf '\n'
export OPENAI_API_KEY
```

zsh:

```zsh
set +x
read -r -s 'OPENAI_API_KEY?OpenAI API key: '
printf '\n'
export OPENAI_API_KEY
```

Keep request content within the task's permitted sharing and the account's
[OpenAI data controls](https://developers.openai.com/api/docs/guides/your-data).

Then opt in: `cw keys set OPENAI_API_KEY --from-env OPENAI_API_KEY` saves the key
and opts in, or list `openai_decisions` under `advisers.enabled` to use the exported
key alone.

### Optional: set up Jev

Jev is hosted by TypeSafe and is not enabled by default. The shipped policy's
comment shows the line that enables it (`jev: jev-1.14.0` under
`consultation.providers`) and the measurement behind the default: across 261
adjudicated consultations and 587 resolved questions, Jev was the only adviser
matching the verified decision in none of 30 splits, was overruled 14 times and
was unavailable 23 times. Snap matched all 587, which suggests deference to Snap
rather than independent verification.

To opt in:

1. Copy [`model-policy.yaml`](../skills/task-orchestration/model-policy.yaml) to
   a private location.
2. Add `jev: jev-1.14.0` under `consultation.providers` in the copy; if it reads
   `providers: {}`, replace `{}` with that mapping. Use an exact version; aliases
   such as `jev-latest` are rejected.
3. Set `TYPESAFE_API_KEY`, as shown below.
4. Start the task with `cw task start --policy /path/to/copy.yaml`. The task binds
   Jev, and its checkpoints follow that binding automatically. For ad hoc
   consultations, use `cw consult --policy /path/to/copy.yaml`. A copy still
   honours `advisers.disabled` in your preferences.

`cw task start` fails before creating a task when the policy or your saved
preferences cannot be read or are invalid, including a policy older than version 3. Obtain a key from the official
[TypeSafe Console](https://console.typesafe.ai/keys).

Bash:

```bash
set +x
IFS= read -r -s -p 'TypeSafe API key: ' TYPESAFE_API_KEY
printf '\n'
export TYPESAFE_API_KEY
```

zsh:

```zsh
set +x
read -r -s 'TYPESAFE_API_KEY?TypeSafe API key: '
printf '\n'
export TYPESAFE_API_KEY
```

TypeSafe is hosted in the US. Zero data retention is available only to enterprise
customers, and its privacy policy states that it does not train on prompts.
The installed [TypeSafe skill](../third_party/typesafe-ai/SKILL.md) and its
[development consultation guide](../third_party/typesafe-ai/references/development-consultations.md)
describe the API contract.

An exact version keeps results comparable when TypeSafe changes its moving
aliases. As checked on 16 October 2026, the official
[model reference](https://docs.typesafe.ai/models) maps `jev-latest` to
`jev-1.13.0` and `jev-preview` to `jev-1.14.0`. The linked
[1.13 limitations](https://docs.typesafe.ai/model-jaggedness/jev-1.13) describe
that version, rather than a verified list of 1.14 limitations. The
[Jev 1.14 announcement](https://typesafe.ai/blog/jev-1-14) also describes opt-in
[images](https://docs.typesafe.ai/images),
[bounding-box answers](https://docs.typesafe.ai/primitives/box) and a long-context
header. The shared helper sends none of these headers and keeps the same text and
Choice questions for every adviser.

### Make keys visible to the agent

Run `cw keys status`. For each key it shows whether the key comes from that
process's environment, the saved keys or neither. These are
offline checks; they do not validate keys or prove availability in another
process. `cw doctor`'s credential lines count other places and point back to
`cw keys status`: `credentials_environment` counts that process's environment
without saved keys, and `credentials_launcher` and `credentials_codex_shell`
count Codex environments, not `cw`'s own. Start the CLI from the same terminal
to inherit exported keys:

```sh
claude
```

```sh
codex
```

Desktop apps start from your login session, not your shell profile. Supply the
keys through your local secret manager or the app's launch environment. On macOS,
after setting them in the terminal, a session-only option is:

```sh
set +x
launchctl setenv SUBQ_API_KEY "$SUBQ_API_KEY"
launchctl setenv OPENAI_API_KEY "$OPENAI_API_KEY"
```

Add `TYPESAFE_API_KEY` the same way if you opted in to Jev. Fully quit and
relaunch the app, then open a fresh session and verify key presence without
displaying values. This launch environment applies to GUI processes started later
in the current login session; it is not persistent secret storage. Check keys
before `cw task start`, because the task binds only the advisers configured then,
and make sure each one is opted in (`cw keys set` or `advisers.enabled`): a key that
is only visible to the agent binds nothing, and `cw doctor` says
"key present, adviser not enabled".

## Consult advisers and let the coordinator decide

Substantial work follows a fixed flow, and no required state is skipped: classify
the task, resolve the before checkpoint, implement and verify, resolve a fresh
final checkpoint, and complete only when both are resolved. The checkpoints are
required when the task has an open decision: real alternatives with consequential
uncertainty. When the approach follows directly from the defect or contract and no
changed code must behave alike on several platforms or shells, the plan declares `"checkpoints": {"required": false, "reason": "..."}`, and
`cw task verify` lists that reason instead of requiring the checkpoints; when in
doubt, it keeps them. That reason must not claim that no advisers are configured
or that adviser credentials are missing while the task binds advisers:
`cw task start` refuses such a plan, and `cw task verify` reports it as a problem
on a record an older `cw` created, unless both checkpoints are attached. Tasks
without implementation use the before checkpoint ahead of substantive drafting or
review. Small edits and direct factual answers remain lightweight.

After the before checkpoint, advisers join the flow only at four decision
points: (a) after the spec
review, on an open question the spec leaves to a choice; (b) on the architect's
choice between real options, before the design review; (c) before the
coordinator rejects a `blocker` or `major` review finding at any stage; and (d)
the final checkpoint before `cw task verify`. When the task's binding holds an
adviser, the coordinator consults it at each point; when it holds none, points a
to c are skipped and recorded as "no advisers configured", point d attaches the
offline `no_advisers_configured` record, and the flow runs as it would without
advisers. The [adviser points](../skills/task-orchestration/references/consultation-helper.md#adviser-points-in-the-flow)
give the commands.

Configured advisers receive the same permitted evidence, questions and neutral
Choice options. The [consultation helper](../skills/task-orchestration/references/consultation-helper.md)
makes one attempt per adviser within a 30-second batch budget, with no automatic
retries, and validates each response independently. Each adviser ends as
`success`, `partial` or `refusal` (OpenAI refusals), `unavailable` (attempted and
failed) or `not_configured` (no key; no request made). Confidence and voting never
choose the result.

See the [decision diagrams](decision-flow.md) for the flow, the adviser
branches, missing coverage, and the coordinator's resolved and unresolved decisions.

Prepare a request containing `{state, questions}` following the
[authoring rules](../skills/task-orchestration/SKILL.md#author-questions). Keep
credentials outside requests, records and shared evidence. The commands use
different output paths because the helper refuses overwrites:

```sh
cw consult --request /path/to/request.json --output /path/to/preview.json --dry-run
cw consult --request /path/to/request.json --output /path/to/advice.json
```

The dry run validates and writes a preview without network access. A live call
can incur usage with configured advisers and leaves `final_decision` pending.
After checking the evidence, the coordinator writes a decision and finalizes a
separate record offline:

```sh
cw consult --adjudicate /path/to/advice.json --decision /path/to/decision.json --output /path/to/final.json
```

Adjudication enforces the [rules](../skills/task-orchestration/SKILL.md#adjudicate)
structurally. A resolved question needs typed `source:`, `check:` or `observation:`
evidence and verification, never adviser output: check IDs, run IDs and
observations cannot name an adviser (including `gpt-6-luna`), while `source:`
paths are exempt. It also needs a reason for every differing adviser and an
investigation note for differing advisers with confidence of 0.7 or higher, in
`partial` as in `disagreement` comparisons; and `agreement_not_independent: true`
when advisers agree on a request that marks untrusted text in `state.untrusted`.
An unresolved decision blocks completion. `cw consult` exits `0` for success, no
configured advisers, a dry run or a resolved adjudication; `2` for partial or
unavailable coverage, an unresolved adjudication, a task-bound adviser without
its key (also in a dry run), or a command-line usage error such as an unknown
flag (including the removed `--with-openai`) or conflicting or missing options,
reported before any file is read; and `1` for an invalid request, record,
decision or policy file or an existing output path. Attaching a new
record for the same phase with `cw task consult` replaces the earlier one, as
long as the record is a new file distinct from the other phase's.

A successful API response proves access for that request; it does not prove
judgment accuracy or implementation correctness.

To check the configured integrations from a fresh agent session, use:

```text
Use the installed task-orchestration workflow to report which advisers are
configured, checking key presence without showing values. If any are configured,
make one Choice request with cw consult using only synthetic public text, and
report each adviser's status or safe failure category and its requested and
returned model. Compare the results with the supplied evidence and record the
coordinator's final decision. Never print keys or authorization headers.
```

See [workflow and integrations](workflow.md#decision-advisers) for policy
details and source links.

### Evaluate multilingual judgments

The bundled [multilingual evaluation](../skills/task-orchestration/references/adviser-providers.md#multilingual-evaluation)
uses 12 synthetic scenarios translated into English, Ukrainian and German.
Expected answers stay with the local scorer. Each adviser is scored independently;
agreement and confidence do not choose the correct answer.

From the editable checkout, validate the corpus offline:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual
```

For an explicit hosted run, supply the keys of the advisers you want evaluated
through the process environment and choose a new private output directory outside
repositories and managed releases:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual --live --output-dir ~/.local/state/agent-workflows/audits/multilingual-baseline-01
```

Only configured advisers run; the report lists the others as `not_run` with reason
`not_configured`, which is not a failure. The live run makes at most three
requests per configured adviser without retries and writes `report.json`,
`summary.md` and safe consultation records. Read accuracy together with coverage
and per-case errors. A valid `insufficient_evidence` judgment is an answer; a
refusal or unavailable adviser is missing coverage. This small labeled baseline
measures performance on its frozen cases. It does not establish calibration,
production readiness or improvement over another model version. Installation,
source validation and CI never run the hosted evaluation.
