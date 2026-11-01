---
name: code-review
description: Review PRs or local diffs for concrete defects, tracing changed behavior and reporting exact evidence and verification limits.
---

# Evidence-based code review

Establish the requested scope and exact base/head or local diff. Inspect status and relevant untracked files so they are neither overlooked nor mistaken for part of the change. Read repository conventions, neighboring code, callers, and tests needed to verify the changed behavior. A review remains read-only unless implementation or an external review action is authorized.

Prioritize defects with a concrete trigger and observable consequence. Trace inputs through changed code into consumers and persisted state when relevant. Check ownership, cancellation and shutdown, partial failures, retries and duplicates, bounds, authorization, secret handling, and API/configuration compatibility where the change touches them. Repository rules and supported environments take precedence over generic style preferences.

Review tests as evidence: a test should fail for the defect it claims to prevent and assert externally relevant behavior. Look for missed negative paths, unreliable timing, over-mocking of the boundary at risk, and checks that merely repeat implementation logic. Run appropriate verification only within the user's scope and repository environment; do not rerun valid evidence solely because it came from an earlier turn. Inspect current code, input, and environment changes to decide whether that evidence still applies.

Check the change against what was asked as a separate pass, because code can follow every standard and still implement the wrong thing. When a spec, issue, or request exists, report requirements that are missing or partial, behavior nobody asked for, and requirements that look implemented but wrong. Keep this separate from code quality, so one doesn't mask the other.

For a refactor, count the concepts a reader must hold. If the count is unchanged, the complexity moved rather than shrank, so prefer a change that makes whole branches or layers disappear. When a test looks weak and the task allows running code, settle it by experiment: in a scratch copy, invert one condition the change adds and run the suite. A mutation that stays green is a finding, so name the missing test. Use [design smells](references/smells.md) for design concerns.

For consequential suspected findings, or an explicit user request, resolve installed `typesafe-ai` and read its entrypoint and development consultation guide from that skill’s root. Give the coordinator candidate findings and exact evidence for a bounded support judgment; it owns calls, batching, and reuse across skills. Routine work needs no consultation. Jev supplies typed advice, not generated conclusions, proof, execution results, or authorization. Independently verify material claims even at high confidence. If consultation is unavailable or inconclusive, state the limit and continue with source evidence.

Prove or discard each suspected finding with the smallest useful read or check. Read existing review threads before preparing external comments and avoid duplicates. Do not invent findings to fill a quota. Separate a definite defect from an unresolved question; style-only preferences are not blocking unless they violate an actual project requirement.

For each actionable finding, provide severity, exact file/line, trigger, impact, and a concrete fix direction. Keep line ranges tight and identify whether the issue is introduced or made reachable by the change. Lead with findings. If none survive verification, say so and state the material validation limits. Do not approve, post, resolve, commit, or push unless that action is authorized.
