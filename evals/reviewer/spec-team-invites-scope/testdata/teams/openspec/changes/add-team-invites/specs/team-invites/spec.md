## Purpose
Team admins invite people to a team by email address, and an invitee joins the team by accepting the invite before it expires.

## ADDED Requirements
### Requirement: Admin sends an invite
The system SHALL let a team admin invite a person by email with `POST /teams/{team}/invites`, creating a pending invite, emailing the invitee a link and returning HTTP 201 with the invite `id`.

#### Scenario: Admin invites a new person
- **GIVEN** `ana@example.com` is an admin of team `blue`
- **WHEN** she sends `POST /teams/blue/invites` with `{"email": "raj@example.com"}`
- **THEN** the response is HTTP 201 with an invite `id`
- **AND** `raj@example.com` receives an email with an invite link

### Requirement: Invite expiry
An invite SHALL expire 7 days after it is sent; accepting an expired invite SHALL return HTTP 410 with the message `invite expired` and SHALL NOT add the invitee to the team.

#### Scenario: Invite accepted after 7 days
- **GIVEN** an invite for `raj@example.com` to team `blue` sent at 2026-05-01T10:00:00Z
- **WHEN** Raj accepts it at 2026-05-08T10:00:00Z
- **THEN** the response is HTTP 410 with the message `invite expired`
- **AND** Raj is not a member of team `blue`

### Requirement: Accepting an invite
Accepting a valid invite SHALL add the invitee to the team with the role `member`.

#### Scenario: Invite accepted within 7 days
- **GIVEN** an invite for `raj@example.com` to team `blue` sent at 2026-05-01T10:00:00Z
- **WHEN** Raj accepts it at 2026-05-08T09:59:59Z
- **THEN** Raj is a member of team `blue` with the role `member`

### Requirement: Invite reminder email
The system SHALL email the invitee a reminder 48 hours before a pending invite expires.

#### Scenario: Reminder before expiry
- **GIVEN** a pending invite for `raj@example.com` sent at 2026-05-01T10:00:00Z
- **WHEN** the time reaches 2026-05-06T10:00:00Z
- **THEN** `raj@example.com` receives a reminder email for the invite
