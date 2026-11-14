---
type: llm
focus: { source: file, path: testdata/profiles/openspec/changes/add-avatar-upload/specs/avatar-upload/spec.md }
---

Both sources agree that `PUT /profile/avatar` accepts `image/png` and
`image/jpeg`; any other content type gets HTTP 415 and the current avatar
stays; an oversized upload gets HTTP 413 and nothing changes; after a
successful upload `GET /profile` returns the new `avatar_url`. The page also
says success returns HTTP 200 with `{"avatar_url": "<url>"}` and `DELETE
/profile/avatar` restores the initials avatar with HTTP 204. The sources
disagree on non-square images (page: reject with 422 `avatar must be
square`; ticket: accept and centre-crop) and give no size limit value
("TBD"). Animated images and moderation are out of scope.

PASS if the file has `## Purpose` and `## ADDED Requirements`; it has
requirements, each with at least one `#### Scenario:` of `- **WHEN**` and
`- **THEN**` bullets naming a concrete input and an observable outcome, for
PNG and JPEG uploads succeeding with HTTP 200 and the `avatar_url` then
returned by `GET /profile`, another content type (such as `image/gif`)
rejected with 415 and the avatar unchanged, and `DELETE /profile/avatar`
returning 204 and restoring the initials avatar; no requirement or scenario
decides non-square handling either way (neither a 422 rejection nor
cropping); and nothing states what the size limit is. Concrete test
fixtures are fine: an input such as "a 20 KB JPEG" or "a 256x256 PNG" used
as an ordinary upload is not a limit. A 413 requirement or scenario whose
input is phrased against the unstated limit ("an image larger than the
upload size limit") with HTTP 413 and the avatar unchanged counts as
concrete enough, since the limit itself is an open question; leaving the
413 case out of the spec is also acceptable.
FAIL if any agreed behaviour above (other than the optional 413 case) is
missing, a scenario has no concrete input or observable outcome, non-square
handling is specified as either reading, the spec states a value for the
size limit or a maximum dimension (for example "at most 5 MB", "larger than
2 MB is rejected", "a 4096 px maximum", or a fixture asserted to be over or
under the limit such as "a 6 MB PNG exceeds the limit"), internal names from
`profile.go` (such as `initialsAvatar` or `Handler`) appear, or animated
images or moderation are specified.
