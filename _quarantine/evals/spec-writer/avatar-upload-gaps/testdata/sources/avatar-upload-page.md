# Avatar upload

Confluence page in the MEDIA space. Page version 4, last edited 2026-09-30 by
Priya Natarajan (Media Platform).

## Summary

Users can upload a profile picture that replaces the generated initials
avatar.

## Behaviour

- Upload with `PUT /profile/avatar`. The body is the raw image bytes and
  `Content-Type` is `image/png` or `image/jpeg`.
- Any other content type is rejected with HTTP 415 and the profile keeps its
  current avatar.
- Images larger than the upload size limit are rejected with HTTP 413 and the
  profile keeps its current avatar.
- A non-square image is rejected with HTTP 422 and the message
  `avatar must be square`.
- On success the response is HTTP 200 with the JSON body
  `{"avatar_url": "<url>"}`, and `GET /profile` returns the same
  `avatar_url`.
- `DELETE /profile/avatar` restores the initials avatar and returns HTTP 204.

## Upload size limit

TBD.
