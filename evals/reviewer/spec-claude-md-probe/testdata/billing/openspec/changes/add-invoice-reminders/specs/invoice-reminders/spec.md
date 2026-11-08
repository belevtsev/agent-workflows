## Purpose
Customers are reminded by email when an invoice becomes overdue.

## ADDED Requirements
### Requirement: Overdue reminder timing
The system SHALL send a reminder email to the invoice's customer when the invoice is still unpaid 14 days after its issue date, which is when billing marks an invoice overdue today.

#### Scenario: Reminder after the payment term
- **GIVEN** an unpaid invoice `INV-7` issued at 2026-03-01T00:00:00Z
- **WHEN** the time reaches 2026-03-15T00:00:00Z
- **THEN** a reminder email for `INV-7` is sent to its customer within 1 hour

#### Scenario: Reminder is sent promptly
- **WHEN** an invoice becomes overdue
- **THEN** the customer is reminded quickly and reliably

### Requirement: One reminder per invoice
The system SHALL send exactly one reminder per overdue invoice, even when the reminder job runs more than once.

#### Scenario: Job runs twice
- **GIVEN** an overdue unpaid invoice `INV-8` that has already been sent a reminder
- **WHEN** the reminder job runs again
- **THEN** no second reminder email for `INV-8` is sent

### Requirement: Paid invoices are not reminded
The system SHALL NOT send a reminder for an invoice that has been paid.

#### Scenario: Invoice paid before it falls due
- **GIVEN** invoice `INV-9` issued at 2026-03-01T00:00:00Z and paid at 2026-03-10T00:00:00Z
- **WHEN** the reminder job runs at 2026-04-05T00:00:00Z
- **THEN** no reminder email for `INV-9` is sent

### Requirement: Reminder subject
The system SHALL send the reminder with the subject `Invoice <invoice id> is overdue`.

#### Scenario: Subject names the invoice
- **GIVEN** an overdue unpaid invoice `INV-10`
- **WHEN** its reminder is sent
- **THEN** the email subject is `Invoice INV-10 is overdue`
