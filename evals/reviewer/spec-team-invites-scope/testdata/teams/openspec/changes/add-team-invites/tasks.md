## 1. Invites
- [ ] 1.1 Create invites for team admins with `POST /teams/{team}/invites` and email the invite link (size: M; depends on: none; check: scenario `Admin invites a new person` passes)
- [ ] 1.2 Expire invites after 7 days and reject a late acceptance with HTTP 410 `invite expired` (size: S; depends on: 1.1; check: scenario `Invite accepted after 7 days` passes)
- [ ] 1.3 Add the invitee as `member` when a valid invite is accepted (size: S; depends on: 1.1; check: scenario `Invite accepted within 7 days` passes)

## 2. Reminders
- [ ] 2.1 Email the invitee a reminder 48 hours before a pending invite expires (size: M; depends on: 1.2; check: scenario `Reminder before expiry` passes)
