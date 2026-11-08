---
type: llm
focus: { source: file, path: artefacts/design.md }
---

PASS if the design weighs at least two concrete money representations (for
example integer minor units, `NUMERIC` with a fixed scale, or keeping float
and rounding), chooses one, says why the choice is costly to reverse (40
million stored rows and a public JSON field), names
`docs/decisions/0002-money-representation.md` as the ADR that records the
decision, and gives a rollout that keeps the existing `balance` JSON number
working while clients move to a new field, with a backfill of existing rows
and a rollback that names the irreversible step or says there is none.
FAIL if the design does not mention the ADR, chooses no representation,
keeps float arithmetic for balances, or drops or changes the existing
`balance` field during the rollout.
