## Why
Team admins can only add people by asking support (TEAM-210).

## What Changes
- Admins invite a person to a team by email with `POST /teams/{team}/invites`.
- Invites expire after 7 days; a late acceptance returns HTTP 410.
- Accepting a valid invite adds the invitee as a `member`.
- The invitee gets a reminder email 48 hours before the invite expires.

## Capabilities
### New Capabilities
- `team-invites`: inviting people to a team by email.

### Modified Capabilities
None.

## Impact
New invite endpoint and acceptance flow in the teams service; uses the
existing admin check.

## Non-goals
- Bulk invites.
- Changing an existing member's role.

## Sources
- TEAM-210, updated 2026-10-08.

## Open Questions
None.
