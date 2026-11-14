---
name: backend-security-review
description: Review backend attack paths and dependency risks with source evidence; use code-review for ordinary correctness.
---

# Backend security review

Establish the requested paths, exact revision or base/head, supported environment, and permitted execution. A review remains read-only unless fixes or external actions are authorized. Preserve no-tests and existing-CI-only scope. Identify the exposed entrypoint, attacker-controlled input, authenticated identity, required authority, and protected resource; authentication alone does not bind a caller to a resource owner.

Trace candidate abuse paths through callers, validation, authorization, persistence, and effects. Confirm the actual control and deployment assumptions before ranking impact. A suspicious API, scanner match, or missing defense is a lead, not proof of a reachable defect. Resolve a candidate with the smallest useful source read or permitted check, including evidence that could disprove it.

Choose installed tools and existing CI evidence for the claim at issue using [security evidence](references/security-evidence.md). Do not install tools, select `@latest`, run dependency-mutating commands, or expand the task into a scan campaign. Missing tools leave an explicit evidence gap; continue with source and applicable existing CI. For dependency findings or provenance concerns, read [dependency assessment](references/dependencies.md).

For Go-specific implementation guidance, resolve `cc-skills-golang:golang-security` from the available catalog and load only the relevant domain reference. When working from this suite's source, its [Go security entrypoint](../../third_party/cc-skills-golang/skills/golang-security/SKILL.md) is the source-relative fallback. This skill's execution and tool constraints govern that reuse. The procedure above works without another skill or plugin. Use installed Trail of Bits specialties only when their differential, API-footgun, or supply-chain analysis answers a real unresolved question; do not vendor them. A requested broader threat model belongs to `security-threat-model`; ordinary diff correctness belongs to `code-review`.

Lead the chat result with surviving findings. Each needs severity, concrete trigger and impact, exact path/line and SHA or local-diff identity, the supporting trace or check, a fix direction, and material verification limits. Separate proven defects, unresolved evidence gaps, and optional hardening. If no findings survive, say so without implying that unexecuted scans, tests, CI, or deployed behavior passed. Review submission and messages require their own authorization.
