# Decision record

Keep this record in the artifact or repository location agreed for the task. Populate only useful sections; it is not a mandatory template for small edits.

- **Outcome and constraints:** observable behavior, scope, workload, supported deployment model, and assumptions that affect the design.
- **Current evidence:** exact code or documentation references, relevant revisions, and any disagreement between specification and implementation.
- **Ownership and interfaces:** components, authoritative state, dependencies, call/data direction, and affected consumers. Include a compact editable `archify` diagram when useful.
- **Decision:** the chosen change, why it meets the constraints, rejected alternatives that explain a real tradeoff, and consequences.
- **Consultation, when used:** the configured advisers' focused question or request reference, evidence provenance, resolved model, advisory answer/probabilities and confidence when supplied, the agent's accepted or rejected disposition, and independent verification. Record unavailable or inconclusive consultation and the evidence-based fallback; advice is distinct from required review and verification results.
- **Failure and compatibility:** partial success, retry/duplicate handling, restart recovery, bounded work, API/storage evolution, mixed-version requirements, and rollback where applicable.
- **Implementation and evidence:** independently reviewable steps, dependency/deployment order, required checks and environment, and what each check proves.
- **Completion:** delivered artifacts, observed results, unresolved decisions, and material limitations. Distinguish local fixtures from integration, CI, and production evidence.

Prefer source links to copied package maps that will become stale. Keep product feature runbooks in the owning repository's documentation, not in a reusable skill. Do not write personal memory without an explicit request.
