# Focused compact delivery: offline verification

17 November 2026. This change removes avoidable delivery work from the compact
instructions and shortens generated native handoffs. **It does not establish
real token savings or model-quality equivalence. No paid model was invoked.**
Compact remains opt-in; ordinary task starts are unchanged.
The host chooses observations using this guidance. Explicit compact commands
retain their existing behavior; model compliance has not been measured here.

## Changes

- Worker assignments retain the complete original behavioral contract and
  role-specific work. Their saved packets already contain role instructions,
  selected skill paths/hashes and selective observation policy. Coordinator
  scheduling, budgets and interval/state supervision stay with the coordinator.
- Workers need not reread the full coordinator skill and compact reference.
  Selected original skills and explicit task references are exceptions and
  remain complete. Kitsu never rewrites or deduplicates assignment bytes.
- The generated launch message retains full-packet hash verification, fresh
  context, model/effort, permissions and a minimal usable return shape. Optional
  claims can be omitted, avoiding extra discovery just to populate them.
- Quiet declared checks use ordinary output; noisy output can use compact
  previews and necessary retained-log expansion. Workers do not repeat managed
  baseline/role checks. State observations wait for a task folder or successful
  small-task preparation and an actual need for additional state.
- Return guidance distinguishes gate state `satisfied` from runner outcome
  `passed`, and selected skills from support-guide reads. Validation stays strict;
  an invalid return neither saves claims nor releases the interval.

## Scripted comparison

The replay uses four fresh scripted worker dispatches, real local Kitsu commands,
isolated Git fixtures, one original selected skill per worker and local fake
checks. Both paths load the coordinator guides once and each worker reads its
complete packet, original skill and primary source/test files.

The duplicated path additionally carries coordinator supervision in assignments,
rereads both full guides in every worker, forces compact envelopes for quiet
checks and makes one premature state request. The focused path uses the revised
guidance. This reproduces the **pattern of avoidable work**, not the exact private
historical transcripts, an optimized ordinary baseline or a provider model.
Both paths use the current generated prompt; the table therefore does not
attribute a separate before/after effect to shortening that prompt.

One local execution produced the [sanitized summary](compact-delivery-replay.json):

| Recorded transport | Duplicated compact | Focused compact |
| --- | ---: | ---: |
| Calls | 57 | 48 |
| Request bytes | 17,907 | 14,835 |
| Response bytes | 199,066 | 87,471 |
| Full native reads | 26 | 18 |
| Actual local check executions | 4 | 4 |
| Worker returns / validated finishes | 4 / 4 | 4 / 4 |
| Rejected premature state requests | 1 | 0 |
| Expansions needed by this quiet fixture | 0 | 0 |

The nine removed calls are eight duplicate guide reads and one premature state
request. Required source and selected-skill reads, all four checks, returns and
finishes remain. Payload accounting includes CLI requests/stdin, full JSON
responses, fake-native dispatches, original packet/skill/source reads, handbacks,
follow-ups and the rejected request. Every counter reconciles against the full
in-memory transport ledger. Temporary path lengths and generated identities can
change exact bytes between local executions.

## What was verified

Both paths recover equal original behavioral facts, source/test text, role
model/effort, write limits, selected-skill hashes and worker constraints.
Existing packet tests compare complete original Codex role text exactly once,
or require the Claude native role without duplicating its body. Assignments are
still retained byte for byte. Generated minimal return JSON is parsed and
actually ingested for both Codex and Claude; neither ingestion closes a stage
nor grants review or completion authority.

Separate regressions replay both observed claim mistakes: an unselected task
guide in `loaded.skills`, and `passed` in place of a current `satisfied` check
state. Both are refused without a saved return or released interval. A valid
minimal return and evidence-based finish then succeed.

This delivery fixture starts from passing source and measures no code repair or
final task commit. The existing lifecycle replay separately exercises meaningful
red/green, review findings, repair, fresh review and a real hooked Git commit.
Interrupted-before-Git and stale/unknown/cancelled-worker tests remain required.
Existing noisy-log, last-match and long UTF-8 replays still require and count
all necessary expansions and rejected-page retries. None of these scripted
facts measure model judgment or semantic code quality.

## Run locally

```sh
GOWORK=off go test ./internal/cli ./internal/roles -count=1
GOWORK=off go test ./internal/cli -run '^TestCompactDeliveryOfflineReplay$' -count=1 -v
```

For a new sanitized receipt, set `KITSU_TEST_DELIVERY_REPORT` to a new absolute
file path for the second command. The exporter refuses an existing file and
publishes no raw ledger paths or provider credentials. Provider tokens remain
`null`, and the summary labels full-workflow measurement as false. Do not replace
historical receipts or frozen paid-experiment binaries to rerun this offline
check. No Codex/Claude CLI, adviser or model accounting probe is used.

Local validation passed: Linux build, vet and the full race-enabled suite;
golangci-lint 2.15.0 and govulncheck 1.9.0 for Linux, Darwin and Windows targets;
generated help, Markdown/changelog and OpenSpec record checks. No CI result or
native macOS/Windows runtime pass is claimed.

The baseline for this implementation is main
`e5d8c17db6ff98aca59c1d7ae9b1590aa3d2cc7d`. Historical paid studies are unchanged.
Verify a narrower improvement offline first; any later paid comparison needs a
separately approved task design and budget. Keep ordinary mode as the default.
