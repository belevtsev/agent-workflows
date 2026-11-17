# Optional workflow insights

## Why

Captured token records, local check attempts and managed worker intervals
already exist, but inspecting them separately obscures incomplete attribution,
repeated failures and the difference between estimates and observations.
Consequential assumptions and troubleshooting hypotheses can also lose their
sources when passed between workers.

## What changes

- Add explicit read-only `usage show --diagnostics` with original references,
  exact captured totals, bounded optional note projections and honest timing
  and completeness limits.
- Offer optional consequential claim and decision notes in existing proposal
  and design files; retain arbitrary prose and neutral independent review.
- Offer optional troubleshooting experiments in existing `tests.md`; group
  repeated failures only by the same recorded check, content and policy.
- Record optional token/second estimate ranges and sourced declared limits
  through `usage budget`, separate from check configuration and permissions.

## Not part of this change

No new stage, role, universal checklist, mandatory note schema, autonomous
retry, check suppression, review relaxation, hard cap, runtime cancellation,
price calculation, token conversion or model downgrade. Recording budget
metadata authorizes no provider calls. Compact delivery stays opt-in. No paid
model, pilot or probe runs, frozen experiment changes, or general quality,
token-saving or billing claims are included.

## Open questions

None. Local validation and its limits are recorded in `tests.md`.
