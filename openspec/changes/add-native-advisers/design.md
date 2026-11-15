# Design: add-native-advisers

Extend the existing adviser lifecycle with native text decisions. A consultation still has one
request, one attempt, one saved answer, and explicitly appended dispositions.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Adviser provider configuration | Resolve explicit native definitions into effective settings | existing adviser configuration | no | Preserve strict TOML parsing, whole-definition precedence, and role policy |
| Native HTTP adapter | Return one validated typed answer from a bounded HTTPS attempt | standard HTTP/JSON packages, adviser request types | yes | Existing subprocess execution cannot express typed HTTP calls; keep it for CLI advisers |
| Adviser history | Preserve portable consultation evidence | existing rooted record storage and locks | no | Extend versioned records instead of creating a second task history |
| Adviser CLI | Broker access-controlled consultations | existing session snapshots and adviser package | no | Reuse consult, bindings, resolve, status, check, and JSON output |
| Adviser usage reporting | Report provider counts outside host totals | existing consultation reader and token presentation | no | Preserve host aggregation and add separate native counts |

## Interfaces

Version 2 TOML adds `provider`, `base_url`, and `api_key_env`; `model` and `timeout` remain.
Preset defaults are resolved before binding. `systemone` requires explicit model, URL, and
credential variable. Commands and native definitions are mutually exclusive. Credentials are
read only at consultation preparation and never copied into a binding or result.

Version 2 requests add `score` with 2–10 ordered `levels`, and `boolean` with optional
`criteria.true`/`criteria.false`. Native `decision` accepts 2–255 choices. The adapter sends the
saved rendered prompt as `state` and one named `result` question. Its exact response is the
saved answer; normalized results use probability entries with IDs as values, preserving case.
Booleans expose probability of true with no implicit threshold; scores retain fractions.

Preparation checks capabilities, credentials, and encoded size before creating pending records.
HTTP calls use the existing timeout range, cancellation, HTTPS only, no redirects, no retries,
and bounded bodies. Authentication, credits, limits, malformed answers, and transport errors
retain safe terminal outcomes. Raw HTTP error bodies are discarded. Context freshness is checked
before and after the attempt. Response credentials, including JSON-escaped strings, are refused.

Version 1 remains readable with unchanged CLI fingerprints. Native identities make new bindings
version 2. Native or version 2 request consultations carry version 2 request/result records;
generic disposition records and the binding pointer keep their unchanged format. Offline reads
derive the normalized native result and usage again from the saved provider response, refusing
inconsistencies. Key rotation changes no identity; nonsecret setting drift requires refresh.

The provider catalog and all discovery/check commands stay offline. Native usage is summed
separately; CLI or missing usage remains unknown. Overflow marks aggregates partial rather than
wrapping. No monetary prices or inferred token counts are introduced.

## Rules

| Rule | Checked by |
| --- | --- |
| Presets are offline and grant no implicit access | test |
| CLI configurations, fingerprints, and records remain compatible | test |
| Only supported, bounded, single-question requests can execute | test |
| Typed answers preserve zero values and validate against the request | test |
| HTTPS attempts preserve cancellation, timeout, and no-retry behavior | test |
| Credentials and HTTP error bodies never enter execution records | test |
| Dispositions and current-content checkpoints retain their authority | test |
| Native usage stays separate from host totals and missing data stays unknown | test |
| Host files match source instructions within existing prompt budgets | test |
| Only native advisers import network libraries; gate and tokens retain offline dependencies | test |

The adviser production-code budget increases from 3,500 to 4,000 lines for the native protocol,
response validation, and usage reporting. Other implementation and prompt budgets are unchanged.
