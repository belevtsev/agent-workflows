# Advisers

Advisers are **optional and experimental**, with **none enabled by default**. The normal Kitsu
workflow runs without adviser setup, credentials, or required adviser checkpoints. Their effect
on code quality, cost, and completion time has not been evaluated.

Opting in requires an adviser definition and repository role access, followed by a task binding
and an explicit consultation. API keys, personal definitions, and listing built-in presets grant
no access on their own. Required checkpoints are a separate opt-in.

An adviser answers a bounded question. Built-in TypeSafe Jev and SubQ Snap integrations return
structured choices, scores, and yes/no probabilities through their APIs. Existing CLI advisers
provide text advice or compare named choices using their own authentication. Both paths record
the answer and the coordinator's decision. Kitsu stores no API keys and supplies no model
fallback or automatic retry. An adviser can be from the same provider as the role or another.

The coordinator brokers every consultation. A role asks for advice, returns control, and resumes
with the recorded answer. Advice leaves the role's job and write limits in place. A reviewer
first forms its own assessment and receives none of the author's consultations as review guidance.
Advice supplies neither an independent review nor the developer's approval.

<img src="diagrams/adviser-opt-in.svg" alt="Experimental advisers are disabled by default. Define Jev, Snap or a CLI adviser, grant repository role access, and bind the task. An allowed role supplies an explicit question and context; the coordinator checks scope, consults the API or CLI, records the answer and decision, and resumes the role within its existing limits. Required checkpoints are a separate opt-in: bind or refresh their policy, obtain a successful receipt for the checkpoint and current content, record a disposition with a reason, and check it before proceeding." width="100%">

The first row enables consultations; it makes no provider call. The second row runs only for an
explicit request. The third row applies only when you also configure required checkpoints;
otherwise it is skipped. A checkpoint receipt must match its bound policy and current stage
content, and have an `accept`, `reject`, or `defer` decision with a reason. Independent reviews
and your specification/design approval still apply.

## Built-in decision advisers

Discover presets without credentials, network access, or a repository:

```sh
kitsu advisers providers
kitsu advisers providers --json
```

| Provider | Default model | Default base URL | Credential environment variable |
| --- | --- | --- | --- |
| `typesafe` | `jev-1.14.0` | `https://api.typesafe.ai/v1` | `TYPESAFE_API_KEY` |
| `subq` | `subq-snap-1.2` | `https://api.subq.ai/v1` | `SUBQ_API_KEY` |
| `systemone` | required | required | required |

These are configuration presets, not enabled advisers. The JSON catalog declares
`experimental: true` and `enabled_by_default: false`; listing it enables nothing.
The preset defaults follow the [TypeSafe model documentation](https://docs.typesafe.ai/models)
and [SubQ model documentation](https://docs.subq.ai/using-the-api/models/). The common text API is
documented by [TypeSafe](https://docs.typesafe.ai/api/system-one) and
[SubQ](https://docs.subq.ai/endpoints/decisions-api/). The `systemone` provider supports that same
request/response protocol at another endpoint; it is not a generic chat-completions adapter.

Set the relevant environment variable in the process running Kitsu, using a key from your
provider account. Do not put the key in TOML. Add definitions and explicit access to
`kitsu-advisers.toml`:

```toml
version = 2

[advisers.jev]
provider = "typesafe"

[advisers.snap]
provider = "subq"

[access]
architect = ["jev", "snap"]
coordinator = ["jev", "snap"]
```

No role gains access just because a key is present. You can override `model`, `base_url`,
`api_key_env`, and `timeout`. The base URL includes the API prefix (`/v1` in the presets);
Kitsu appends `/systemone`. It must use HTTPS with no embedded credentials, query, or fragment.
Redirects are refused. `systemone` requires all three of `model`, `base_url`, and `api_key_env`.
Native definitions cannot include `command`; CLI definitions omit `provider`. Version 2 files
can mix both kinds. Personal definitions still cannot grant access or require checkpoints.

Bind a task, then submit one typed question using the existing command:

```sh
kitsu advisers bind --task native-advice --workflow small
kitsu consult --task native-advice --role coordinator --stage code \
  --adviser jev --request - <<'JSON'
{"version":2,"kind":"boolean","question":"Does this design define cancellation behavior?","context":["README.md"]}
JSON
```

This consultation makes a paid provider request using only the supplied question and explicit
context. Use a file that actually describes the behavior you want evaluated. Resolve the returned
consultation id with `accept`, `reject`, or `defer` and a reason, as with CLI advice.

| Request kind | Required additional fields | Native result |
| --- | --- | --- |
| `decision` | `choices`: 2–255 distinct `{id,text}` options | Selected choice, per-option probabilities, confidence |
| `score` | `levels`: 2–10 nonempty descriptions in ascending order | Fractional expected score on zero-based levels, probabilities, confidence |
| `boolean` | None; optional `criteria.true` and `criteria.false` descriptions | Probability of yes, from 0 to 1 |

`score` and `boolean` require request version 2. Existing version 1 decisions also work with
native providers. CLI advisers support `advice` and `decision`; native providers reject `advice`
before creating an attempt. `advisers list`, including `--task`, shows supported `kinds`.

```json
{"version":2,"kind":"score","question":"How completely does the design cover retries?","levels":["Missing","Partial","Complete"],"context":["README.md"]}
```

Human output renders the typed answer; `consult --json` retains the exact successful response
in `execution.answer` and normalized fields in `execution.result`. Probability entries use
`{id,probability}` pairs, so choice ids preserve case. Zero probabilities, zero confidence, and
fractional scores remain distinct from missing fields. Yes/no answers are probabilities, not
automatic pass/fail judgments. Confidence does not grant approval or resolve a consultation.

Requests, rendered context, encoded HTTP bodies, and responses are bounded to 1 MiB. Provider
token limits may be smaller; Kitsu reports rejection without truncating context. It makes one
attempt with the configured timeout, retains HTTP failures without their raw error bodies,
and never retries or switches models automatically. Missing credentials and unsupported kinds
fail before execution and create no pending attempt. Successful API responses that echo the
credential are discarded. Images, batched questions, and SubQ Savant are outside this integration.

## Configure definitions and access

Personal definitions live at `os.UserConfigDir()/kitsu/advisers.toml`. That means
`$XDG_CONFIG_HOME/kitsu/advisers.toml` (normally `~/.config/kitsu/advisers.toml`) on Linux,
`~/Library/Application Support/kitsu/advisers.toml` on macOS, and
`%AppData%\kitsu\advisers.toml` on Windows. The personal file accepts only `version` and
`[advisers.<name>]` definitions.

The repository's optional `kitsu-advisers.toml` defines advisers and owns `[access]` and
`[[checkpoints]]`. A repository definition replaces the whole personal definition with the same
name; fields are never merged. With neither file present, advisers are disabled.

For example, a personal file can define two already authenticated CLIs:

```toml
version = 1

[advisers.claude]
model = "opus"
command = [
  "claude", "--print", "--model", "{model}", "--effort", "high",
  "--input-format", "text", "--output-format", "text", "--tools", "",
  "--safe-mode", "--permission-prompts", "none", "--no-session-persistence"
]
timeout = "5m"

[advisers.codex]
model = "gpt-6.1-sol"
command = [
  "codex", "exec", "--model", "{model}",
  "-c", 'model_reasoning_effort="high"',
  "--sandbox", "read-only", "--ephemeral", "--color", "never",
  "--output-last-message", "{answer_file}", "-"
]
```

These command shapes were checked against Claude Code 2.1.325 and Codex CLI 0.169.0 help; no live
adviser run is claimed. Select a model your CLI account can use. On Windows, use an executable
directly: `.cmd` and `.bat` launchers are refused.

Adviser reasoning is selected through command arguments. It is independent of the requesting
role's effort, and Kitsu does not translate effort settings between providers:

| Adviser CLI | Model field | Reasoning arguments in `command` |
| --- | --- | --- |
| Claude Code | `model = "opus"` | `"--effort", "high"` |
| Codex | `model = "gpt-6.1-sol"` | `"-c", 'model_reasoning_effort="high"'` |

Replace `high` with a supported level to choose another effort. The Codex configuration override
is one argument after `-c`; its double quotes are part of that argument. The TOML single quotes
preserve them without a shell. There is no Kitsu adviser `effort` key. Changing reasoning arguments
changes the definition fingerprint, so an existing task needs an explicit binding refresh.

The repository can then set its policy without repeating those commands:

```toml
version = 1

[access]
default = []
coordinator = ["claude", "codex"]
architect = ["claude"]
reviewer = ["codex"]
```

To additionally require adviser answers at particular stages, explicitly append checkpoints.
Kitsu creates no required adviser checkpoints by default:

```toml
[[checkpoints]]
id = "design-alternatives"
stage = "design"
role = "architect"
advisers = ["claude"]

[[checkpoints]]
id = "code-review"
stage = "review"
role = "reviewer"
advisers = ["codex"]
```

Each role's explicit access list replaces `default`, including an empty list. A role without an
entry uses `default`; without either, access is denied. Valid roles are the coordinator and the
seven [specialist roles](roles.md). Each checkpoint names an allowed requester and one or more
defined advisers. Its stage is `spec`, `design`, or `review`.
Small workflows apply only review checkpoints because they have no specification or design stage.

Configuration is strict version 1 or 2 TOML: unknown fields, unknown references, duplicates, empty
required models, and invalid timeouts are errors. Native fields require version 2.
Adviser names and checkpoint ids use 1 to 40 lowercase
letters, digits, underscores, or hyphens, starting with a letter or digit. `command` is an argument
array, executed directly without shell expansion. Exactly one argument must be the literal
`{model}`; an optional argument may be `{answer_file}`. Each placeholder occupies its whole
argument after the program. The timeout defaults to five minutes and must be from one second to
one hour.

## See the selection in a table

```sh
kitsu roles --host codex
kitsu roles --host claude
kitsu advisers list --role architect
```

The compact roles table shows **Role**, **Model**, **Reasoning**, and **Advisers** for the selected
host. Its coordinator row identifies the host session: Kitsu has no configured coordinator model
or effort. Specialist model and effort settings come from `kitsu-roles.toml`; adviser access comes
from `kitsu-advisers.toml`, or from the saved binding when `--task` is given. See the
[default selection table](roles.md#the-roles-at-a-glance) and [role configuration](roles.md#kitsu-rolestoml).
Without `--host`, `kitsu roles` shows both hosts. There is no interactive model picker.

## Bind a task

```sh
kitsu advisers list --json
kitsu advisers bind --task add-order-csv-export --workflow full
kitsu roles --host claude --task add-order-csv-export
kitsu advisers list --task add-order-csv-export --role architect --json
```

Binding saves the task's workflow, adviser model names and definition fingerprints, access lists,
and checkpoints. It copies neither command arguments nor environment variables. The shared
binding lets another developer or offline CI inspect the record without your personal file or
provider CLI.

An existing binding keeps its policy when current configuration changes. To adopt those changes,
run `kitsu advisers bind --task <task> --workflow full|small --refresh`. Refresh creates a new
revision and preserves the history. A consultation can run only when the locally available
adviser's model and definition fingerprint match the active binding. A changed model or command
cannot silently execute an older selection.

Full tasks keep records under `openspec/changes/<task>/advisers/`; small tasks use
`openspec/changes/.advice/<task>/`. To promote a small task, create its full change folder and run
`kitsu advisers bind --task <task> --workflow full --refresh`. This moves the whole adviser history
before refreshing the binding. The destination adviser folder must be absent. Two existing
histories for one task are an error.

## Ask a question or compare choices

The request is one UTF-8 JSON document with `version`, `kind`, `question`, and optional `context`.
Version 2 also supports the native score and boolean requests described above.
An `advice` request has no choices. A `decision` request has at least two distinct choices, each
with a nonempty `id` and `text`.

To ask a question without attaching files to a bound task:

```sh
kitsu adviser ask codex --task add-order-csv-export --role coordinator --stage code --request - <<'JSON'
{"version":1,"kind":"advice","question":"What failure cases should an order CSV export cover?"}
JSON
```

For a decision with context, the following example uses a full task with an existing `proposal.md` and `design.md`.

```json
{
  "version": 1,
  "kind": "decision",
  "question": "Which approach meets the requirements with less operational work?",
  "choices": [
    {"id": "reuse", "text": "Extend the existing worker."},
    {"id": "separate", "text": "Add a separate worker."}
  ],
  "context": [
    "openspec/changes/add-order-csv-export/proposal.md",
    "openspec/changes/add-order-csv-export/design.md"
  ]
}
```

Save this as `request.json` outside the worktree, then let the coordinator run:

```sh
kitsu consult --task add-order-csv-export --role architect --stage design \
  --adviser claude --checkpoint design-alternatives --request /tmp/request.json
kitsu advisers resolve --task add-order-csv-export <consultation-id> \
  --disposition accept --choice reuse --reason "Reuses the existing retry and shutdown behavior."
```

`--request -` reads the JSON from standard input. `kitsu consult --help` describes the schema.
`--role` identifies the requester, even though the coordinator executes the call. Ad hoc requests
may use any existing task stage: `lookup`, `spec`, `spec-review`, `design`, `design-review`, `tests`,
`code`, `review`, or `commit`. Only a call with the matching `--checkpoint` can satisfy that
checkpoint.

Context consists only of explicitly listed repository-relative regular files. Absolute paths,
paths outside the repository, duplicate paths, and symlinks that escape it are refused. Kitsu
renders those files into the prompt and sends no other file automatically. The request and
rendered prompt must each fit in 1 MiB. The answer must be nonempty UTF-8 and fit in 1 MiB.
With `{answer_file}`, the CLI must create the supplied new file; stdout is not a fallback.

The coordinator checks the requesting role's writes with `kitsu roles check` before the call so
adviser records do not become that role's writes. It sends the answer and consultation id back
to the requester, then records `accept`, `reject`, or `defer` with a nonempty reason. `--choice` is
optional; when supplied, it must name an offered choice. Record it when choosing a named option;
a free-form answer does not imply a selection. Dispositions append to the history; the original
prompt, answer, and execution metadata stay unchanged.

The record preserves failed calls, timeouts, cancellations, empty answers, and output overflows.
They satisfy no checkpoint. Kitsu attempts no retry and selects no replacement adviser. Correct
the cause and make an explicit new call when appropriate.

## Check required advice

These checks apply to explicitly configured checkpoints in a task binding. Without them,
the workflow has no adviser requirement; optional consultations still keep their history.

```sh
kitsu advisers status --task add-order-csv-export --json
kitsu advisers check --task add-order-csv-export --checkpoint design-alternatives
kitsu advisers check --task add-order-csv-export
```

A required receipt must match the active binding, checkpoint, requester, adviser identity, and
stage's current content id. It needs a successful answer and an `accept`, `reject`, or `defer`
disposition with a reason. Changed content makes the receipt stale. A checkpoint with several
advisers needs a receipt for each one. Rejecting advice satisfies the requirement to address it;
it does not erase the answer.

Kitsu reads the task's artifact ids before rendering the prompt and checks them again before
calling the provider. A detected change is refused without a provider call or pending history.
It checks again after execution; changed artifacts prevent a successful receipt.

`kitsu change status` holds a full task at the corresponding `spec`, `design`, or review stage until
its checkpoints are satisfied. `kitsu change check` validates adviser record structure on both
paths without requiring advice for future stages. With no task named it also discovers small
histories under `.advice/`; `kitsu change check <small-task>` needs no full change folder.
`kitsu change status` applies to full tasks; use `kitsu advisers status` for small-task readiness.
`kitsu advisers check` compares receipts with the current
artifacts; status can recognize a completed task from its committed baseline. Offline checks use
the shared binding and saved records.

Adviser dispositions do not waive a `blocker` or `major` review finding, replace required reviews,
or grant human approval. Apply the existing [review rules](workflow.md#review-rounds) to real
findings. Verify adviser claims against the sources and treat the answer as evidence.

## Shared history and usage

The task's adviser folder keeps binding revisions, immutable consultation records with their
exact rendered prompts and answers, execution outcomes, and appended dispositions. Commit that
folder with the task on either workflow path. Prompts and answers may contain the context you
explicitly supplied; inspect them before publishing. Command arguments, environment variables,
and raw CLI diagnostics are not copied into shared records.

CLI actual model identity and usage remain `unknown`. Native responses supply a provider-reported
model and optional input/output token counts. These are provider claims, not independent model
verification; the requested model alone proves neither identity nor usage. [`kitsu tokens`](tokens.md)
reports adviser usage separately from host totals, marking mixed or missing counts incomplete.
No live model evaluation, comparison of adviser quality, or claim of token savings accompanies
this feature.

Version 1 configurations and records remain readable. Existing CLI fingerprints are unchanged.
Native bindings and consultations use version 2; typed version 2 requests also create version 2
consultations. Earlier records are not rewritten, and older Kitsu versions cannot read the new
records. Native fingerprints include provider, effective model, base URL, credential variable
name, and timeout, but never the credential value. Key rotation needs no binding refresh.

## Command reference

```text
kitsu advisers providers [--json]
kitsu advisers list [--role <role>] [--task <task>] [--json]
kitsu advisers bind --task <task> --workflow full|small [--refresh] [--json]
kitsu consult --task <task> --role <role> --stage <stage> --adviser <name> --request <file|-> [--checkpoint <id>] [--json]
kitsu advisers resolve --task <task> <id> --disposition accept|reject|defer --reason <text> [--choice <id>] [--json]
kitsu advisers status --task <task> [--checkpoint <id>] [--json]
kitsu advisers check --task <task> [--checkpoint <id>] [--json]
```

`providers`, `list`, `bind`, and `status` make no provider calls. `check` changes no records. A failed
consultation is reported as a failure and retained for inspection; successful output alone never
records a disposition.


## Review feature stories at intake with Jev

In `kitsu-advisers.toml`, name a TypeSafe Jev adviser and allow the coordinator
to use it. Keep any other providers and role access already configured.

```toml
version = 2

[advisers.jev]
provider = "typesafe"
model = "jev-1.14.0"

[access]
coordinator = ["jev"]
```

Set `TYPESAFE_API_KEY` in the environment that launches Codex or Claude Code.
In your own terminal in the checkout, run:

```sh
kitsu advisers story-review enable --adviser jev
```

Review the destination and confirm. Future feature intakes with
`--feature-brief` send the saved request, goal, scope, criteria IDs and expected
behavior. The batch has six named questions about the goal and actor, scope,
behavior, testability, failure cases and compatibility. Kitsu sends at most
16 KiB of story data, excluding check commands and logs. The request lasts up
to ten seconds. See the [TypeSafe System One schema](https://api.typesafe.ai/openapi.json).

The task keeps the result and any provider usage on the intake brief revision.
The host uses it to spot gaps and can save a revised brief after clarifying
the request. If the provider is unavailable or the call is interrupted, local
planning can continue. Replaying an intake uses its existing record. Review
and verification requirements still apply. Read older advice with
`kitsu task show ID --json`. The Markdown receipt contains verification evidence.

A change to the selected adviser, coordinator access or repository check
policy requires a fresh opt-in. Run `kitsu advisers story-review disable` in your
terminal to turn it off for future tasks in this checkout.
