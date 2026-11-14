---
type: llm
focus: { source: file, path: testdata/ledger/docs/decisions/0002-money-representation.md }
---

The ADR records a costly-to-reverse choice: how a ledger with about 40
million `DOUBLE PRECISION` rows (`schema.sql`) and a balance API that clients
parse as a JSON number (`api.go`) represents money exactly, for currencies
with 0, 2 and 3 decimals.

PASS if the ADR follows the decision-record format, with sections or clearly
labelled parts for outcome and constraints, current evidence with `path:line`
references into the ledger repository (for example the float64 `Amount` in
`ledger.go` or the `DOUBLE PRECISION` column in `schema.sql`), ownership and
interfaces, the decision with at least one rejected alternative and why, its
consequences, and failure and compatibility (how existing rows move without
losing their recorded value, how the API stays compatible for the mobile app
and the partner export during the rollout, and which step is irreversible or
that none is); names one concrete representation (for example integer minor
units with the currency's exponent, or `NUMERIC` with a fixed scale); and is
numbered and titled in the folder's style, like `0001-postgres-for-the-ledger.md`.
FAIL if the ADR is a copy of a generic template with placeholders, names no
decision, has no rejected alternative, gives no evidence from the repository,
or says nothing about existing rows or API compatibility.
