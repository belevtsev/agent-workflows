---
name: "security-threat-model"
description: "Build a requested repository threat model with source-backed boundaries, realistic abuse paths, and prioritized mitigations."
---

# Repository threat model

Deliver an actionable model for the requested repository and usage, grounded in
source rather than a generic checklist. Establish scope, deployment, exposure,
authority, sensitive assets, and attacker capabilities from supplied context and
maintained evidence. Reuse earlier answers; keep observations, assumptions, and
proposals distinct. Read [prompt and report aids](references/prompt-template.md)
only when discovery or reporting needs their detailed structure. For a control or
asset you cannot classify, use [controls and assets](references/security-controls-and-assets.md).

Trace in-scope entrypoints, components, stores, integrations, and concrete trust
boundary edges with their transport, authentication, authorization, validation,
and isolation. Separate runtime behavior from build/CI/dev tooling and fixtures.
Do not invent architecture, exposure, or existing controls. Describe realistic
attacker capabilities and non-capabilities so impact is proportionate.

Build a small set of consequential abuse paths tied to goals, prerequisites,
boundaries, assets, and exact evidence. Rank likelihood and impact with the
assumptions that materially affect each priority. Distinguish evidenced controls,
control gaps, and proposed mitigations; tie fixes and manual-review paths to the
actual owner and code.

For consequential prioritization or an explicit request, use configured advisers
via `cw consult` if any are configured; otherwise proceed on source evidence under
the same [decision rules](../task-orchestration/SKILL.md#adjudicate). Supply grounded candidate paths and risk judgments to the coordinator,
which owns calls, batching, and reuse. Routine work needs no consultation. Typed
advice is not exploit proof, validated controls, or authorization; verify conclusions
independently. Record unavailable/inconclusive consultation and continue with source
analysis. If advice influences the result, retain request/evidence provenance,
resolved model, answers/probabilities and supplied confidence, disposition, and
independent verification.

Resolve material missing context from evidence before asking. Ask only when it
changes scope or ranking and cannot be established; continue independent work.
State reasonable unresolved assumptions and conditional recommendations without
requiring an approval round. Elapsed time is not an answer or authorization.

Before delivery, check discovered entrypoints and trust boundaries, runtime/tooling
separation, carried user decisions, open questions, and consultation limits.
Respect no-tests/read-only scope and separate advice, source traces, executed
checks, CI, and deployed evidence. Match the requested chat or artifact destination;
use relevant report sections rather than filling a threat quota. External writes
require their own authorization.
