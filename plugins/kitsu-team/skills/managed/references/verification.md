# Task behavioral verification

Read this when a repository opts in to behavioral verification. Keep compact
mode opt-in. Use `kitsu check plan --help` and `kitsu task verify --help` for
the actual syntax; see the
[guide](https://github.com/belevtsev/kitsu/blob/main/docs/verification.md).

`when` schedules relevant declared checks first, `expect` describes observable
behavior and `verify = true` requires applicable verification. `paths` keeps its
dependency/evidence reuse promise. `check plan [--from <rev>]` defaults to HEAD,
compares the complete working tree and reports reasons, remaining full gate
checks and uncovered areas. Mapping never removes full required checks.

Use selected original project skills for prerequisites, actual app startup,
inputs, assertions and cleanup through existing tools. Every fresh quality
engineer, developer and reviewer loads those original instructions and relative
references; receipts prove neither loading nor compliance. Never automatically
discover or execute scripts, install tools, or treat Kitsu as a service manager.
Quality engineers establish the regression; developers exercise behavior;
reviewers assess independently. Workers return observations and gaps only.

Finish every managed worker interval before running
`kitsu task verify <task> --from <base>`; the first attempt requires an explicit
base, later attempts reuse its pinned revision. Add `--workflow small` for a
small task, and `--json` or opt-in `--compact` for presentation. Without an
explicit workflow, an existing full folder selects full; otherwise small. Only the
coordinator command writes summaries; workers never do.

Targeted failure stops the attempt and leaves remaining checks pending. On
targeted success, publish the sanitized historical attempt summary before final
required checks. Reuse only current actual evidence, never base-implied passes.
No tracked writes follow final checks. An unchanged successful repeat creates
no new history or check run.
Reuse requires a matching private observation of actual filesystem bytes and
deciding evidence. First local verification runs applicable required checks
even with older Git-only passes. Changed raw source (including hidden or
normalized edits), policy, selected original skills or deciding records require
fresh behavioral observation. Resolve both hosts' selected sources for the
fingerprint; this changes neither role execution nor the actual chosen host.

Full summaries live in `openspec/changes/<task>/verification/`; small summaries
in `openspec/changes/.verification/<task>/`, with immutable
`attempts/<sha256>.json` and a current `binding.json`. They are source-bound
historical claims. Raw logs stay local. Readiness requires matching current
local records and available logs; a clone without logs reports unavailable,
never passed. `task check` validates structure without private logs. Missing
logs require `task verify` to establish actual local evidence again. Missing
applicable required verification blocks full done; small stage stays unknown
with separate verification status. Tasks without metadata keep existing behavior.

Verification grants no independent review, human approval or gate authority.
Compact and ordinary output share this lifecycle; neither establishes general
quality equivalence, provider-token savings or billing benefit.
