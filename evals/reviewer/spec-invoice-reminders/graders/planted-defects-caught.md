---
type: llm
focus: { source: file, path: artefacts/spec-review.md }
---

The change folder has two planted defects. (1) The scenario `Reminder is
sent promptly` under `Overdue reminder timing` has no input and no
observable outcome ("the customer is reminded quickly and reliably"), while
BILL-51 gives the value "within 1 hour". (2) The requirement `Overdue
reminder timing` (and the proposal's Why) claims billing marks an invoice
overdue 14 days after issue, but the code sets `const paymentTermDays = 30`
at `invoice.go:6` and computes the due instant from it at `invoice.go:17-18`
(`testdata/billing/invoice.go`); BILL-51 asks to use the rule billing
applies today. The scenario `Reminder after the payment term` repeats the
14-day value.

PASS if the review has one entry per finding, each with an ID (`S1`,
`S2`, ...), a severity of `blocker`, `major` or `minor`, a category, a
location (file plus requirement, scenario or task), evidence and what would
resolve it; one finding locates the `Reminder is sent promptly` scenario,
has category "untestable scenario", severity `blocker` or `major`, quotes
its vague outcome as evidence and names the missing value or observable
outcome (such as the 1-hour bound from BILL-51); another finding locates
`Overdue reminder timing`, has category "contradiction with current code",
severity `blocker` or `major`, cites `invoice.go:6` or a line from 16 to 24
of `invoice.go` (the `DueAt` and `IsOverdue` rule built on the 30-day term), and says the 14-day claim must
follow the existing 30-day rule; every finding cites evidence; and the
review ends with the sources read and what was not reviewed.
FAIL if either planted defect is missing, rated `minor`, filed under a
different category, or lacks its evidence (the quoted scenario text for the
first, a `path:line` in `invoice.go` for the second); if the review states
that the current code uses 14 days; if any entry has no evidence; or if
the review rewrites the change folder instead of reporting.
