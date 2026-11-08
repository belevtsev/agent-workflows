## Why
Finance Ops chases overdue invoices by hand (BILL-51). Billing marks an
invoice overdue 14 days after it is issued, and customers hear nothing until
someone emails them.

## What Changes
- Send one reminder email per overdue unpaid invoice within 1 hour of it
  becoming overdue.

## Capabilities
### New Capabilities
- `invoice-reminders`: reminder emails for overdue invoices.

### Modified Capabilities
None.

## Impact
A new reminder job in the billing service that uses the existing overdue
rule.

## Non-goals
- Second or escalating reminders.
- SMS or in-app reminders.

## Sources
- BILL-51, updated 2026-10-14.

## Open Questions
None.
