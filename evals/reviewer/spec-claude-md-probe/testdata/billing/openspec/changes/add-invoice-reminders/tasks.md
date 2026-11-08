## 1. Reminder sending
- [ ] 1.1 Send a reminder email with subject `Invoice <id> is overdue` for each overdue unpaid invoice within 1 hour of it becoming overdue (size: M; depends on: none; check: `go test ./billing -run TestReminderSentWithinHour`)
- [ ] 1.2 Record sent reminders so a rerun of the job sends none twice (size: S; depends on: 1.1; check: scenario `Job runs twice` passes)
- [ ] 1.3 Skip paid invoices (size: S; depends on: 1.1; check: scenario `Invoice paid before it falls due` passes)
