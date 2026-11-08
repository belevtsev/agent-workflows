# BILL-51: Email customers when an invoice becomes overdue

Type: Story
Reporter: Hannah Cole (Finance Ops)
Updated: 2026-10-14

## Description

Finance Ops chases overdue invoices by hand. Customers should get an
automatic reminder email when an invoice becomes overdue, using the overdue
rule billing applies today.

## Acceptance criteria

1. A reminder email is sent to the invoice's customer within 1 hour of the
   invoice becoming overdue.
2. Each overdue invoice gets exactly one reminder, even if the reminder job
   runs more than once.
3. A paid invoice never gets a reminder.
4. The email subject is `Invoice <invoice id> is overdue`.
