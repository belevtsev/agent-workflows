# Bounded coordinator observations

Coordinators need current task facts without repeatedly printing complete task,
check and worker-return records. Extend `task show` with explicit compact
snapshots, then enable section deltas only from retained complete baselines.

Source: owner request to add bounded coordinator state and subsequent known-snapshot deltas.

The view includes recorded stage, active interval, current checks, open recorded
findings, effective policy and known completion prerequisites. Return original
references for detail. Ordinary views and compact opt-in remain compatible.
No new paid pilot or CI wait is required; offline tests verify local behavior.
Native execution, skill loading, semantic TDD, reviews, approvals and hooked Git
completion remain outside the authority of these projections.
