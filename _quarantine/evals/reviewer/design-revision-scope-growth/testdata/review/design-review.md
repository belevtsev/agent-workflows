# Design review: add-webhook-retries (round 1)

Coordinator dispositions are recorded under each finding. Every finding was
accepted and returned to the architect.

## D1 (major, untestable requirement)

- Location: `interface-contract.md` R3.
- Evidence: R3 says only "Retries back off so that a struggling endpoint is
  not overwhelmed", while the spec's `Retry schedule` and HOOK-88 criterion 1
  give the waits 1 s, 2 s, 4 s, 8 s and 16 s. The verification plan leaves R3
  to a manual check.
- Resolve by: stating the schedule in R3 and testing it with an injected
  clock.
- Disposition: accepted.

## D2 (blocker, missing failure mode)

- Location: `design.md` Failure behaviour; `interface-contract.md`.
- Evidence: the spec's `Retries survive a restart` (scenario `Restart between
  attempts`) and HOOK-88 criterion 3 have no contract requirement, and the
  in-memory `time.AfterFunc` timers owned by `Retrier` are lost on a restart,
  while delivery state is in the `deliveries` table
  (`hooks/delivery.go:26-28`).
- Resolve by: a requirement and a failure-behaviour entry for restarts.
- Disposition: accepted.

## D3 (major, option not weighed)

- Location: `design.md` Options considered.
- Evidence: one option only. The existing `Sweeper` already polls pending
  deliveries from the table (`hooks/delivery.go:26-33`, `:53-56`), so a
  persisted next-attempt time read by the sweeper is an option the design
  never weighs.
- Resolve by: weighing that option, with its consequences, against the timers.
- Disposition: accepted.

## Inputs read

`design.md`, `interface-contract.md`, the change folder
`hooks/openspec/changes/add-webhook-retries/`, `sources/HOOK-88.md`,
`hooks/delivery.go`.

## Not reviewed

Nothing.
