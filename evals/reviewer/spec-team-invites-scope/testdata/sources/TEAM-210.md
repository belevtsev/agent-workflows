# TEAM-210: Invite people to a team by email

Type: Story
Reporter: Grace Liu (Collaboration)
Updated: 2026-10-08

## Description

Team admins can only add people by asking support today. Admins should be
able to invite someone by email address.

## Acceptance criteria

1. Only team admins can send invites.
2. `POST /teams/{team}/invites` with `{"email": "<address>"}` creates a
   pending invite and emails the invitee a link. The response is HTTP 201
   with the invite `id`.
3. An invite expires 7 days after it is sent. Accepting an expired invite
   returns HTTP 410 with the message `invite expired`, and the invitee does
   not join.
4. Accepting a valid invite adds the invitee to the team as a `member`.

## Out of scope

- Bulk invites.
- Changing an existing member's role.
