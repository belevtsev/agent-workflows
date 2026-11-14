# MEDIA-88: Let users upload a profile picture

Type: Story
Reporter: Luis Ortega (Profiles team)
Updated: 2026-10-02
Links: Confluence "Avatar upload" (MEDIA space)

## Description

Users keep asking to replace the initials avatar with a photo. The behaviour
is described on the linked page.

## Acceptance criteria

- PNG and JPEG uploads work; other types get 415.
- Oversized uploads are refused with 413 and nothing changes.
- Non-square photos are accepted and centre-cropped to the largest square, so
  users do not have to crop on their phones.
- After an upload, `GET /profile` shows the new `avatar_url`.

## Out of scope

- Animated images.
- Moderation of uploaded pictures.
