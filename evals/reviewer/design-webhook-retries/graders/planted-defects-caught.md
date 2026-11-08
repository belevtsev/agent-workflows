---
type: llm
focus: { source: file, path: artefacts/design-review.md }
---

The design has three planted defects. (1) Untestable requirement: R3 in
`interface-contract.md` says only "Retries back off so that a struggling
endpoint is not overwhelmed", while the reviewed spec (`Retry schedule`,
scenario `Second attempt after one second`) and HOOK-88 criterion 1 give
the schedule 1 s, 2 s, 4 s, 8 s and 16 s; the verification plan even
leaves R3 to a manual check. (2) Missing failure mode: the spec
requirement `Retries survive a restart` (scenario `Restart between
attempts`, HOOK-88 criterion 3) has no requirement in the contract, and
the design keeps pending retries only in in-memory `time.AfterFunc` timers
owned by `Retrier`, so a restart or redeploy loses them; `Failure
behaviour` never mentions a restart, while the current code keeps delivery
state in the `deliveries` table and says deliveries survive a restart
(`hooks/delivery.go:26-28`). (3) Options left unweighed: `Options
considered` lists one option only, with no smallest-change alternative,
although the existing `Sweeper` already polls pending deliveries from the
table (`hooks/delivery.go:26-33`, `:53-56`), so a persisted next-attempt
time read by the sweeper is an obvious option the design never weighs.

PASS if the review has one entry per finding, each with an ID (`D1`, `D2`,
...), a severity of `blocker`, `major` or `minor`, a category, a location
(file plus requirement or section), evidence and what would resolve it; one
finding locates R3, quotes its vague wording and names the missing schedule
from the spec or HOOK-88, severity `blocker` or `major`; one finding says
pending retries are lost on a restart, cites the spec's restart
requirement or HOOK-88 criterion 3 and the in-memory timer design, and asks
for a contract requirement and a failure-behaviour entry for restarts,
severity `blocker` or `major`; one finding says only one option was
considered, cites the existing sweeper or the `deliveries` table with a
`path:line` in `delivery.go`, and asks for the alternative to be weighed;
every finding cites evidence; and the review ends with the inputs read and
what was not reviewed.
FAIL if any planted defect is missing, the R3 or restart finding is rated
`minor`, a finding lacks evidence, the review rewrites the design or
contract instead of reporting, or the review cites the architect's notes
or intent (for example that ops drain deploys for thirty seconds, or that
the schedule is in the implementer's brief) as evidence.
