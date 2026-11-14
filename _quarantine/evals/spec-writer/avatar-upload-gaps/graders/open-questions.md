---
type: llm
focus: { source: file, path: testdata/profiles/openspec/changes/add-avatar-upload/proposal.md }
---

Two sources were given. The Confluence page "Avatar upload" (page version 4,
last edited 2026-09-30 by Priya Natarajan, Media Platform) says a non-square
image is rejected with HTTP 422 and the message `avatar must be square`, and
gives the upload size limit as "TBD". Jira ticket MEDIA-88 (reporter Luis
Ortega, Profiles team, updated 2026-10-02) says non-square photos are
accepted and centre-cropped to the largest square. Neither source gives a
size limit in bytes or a maximum image dimension.

PASS if `## Open Questions` contains (a) a question asking what the upload
size limit is, naming who or what settles it (for example Priya Natarajan,
the Media Platform team, the page owner, or a named decision record) and not
presenting any number as decided; and (b) one question that records both
readings of non-square handling, rejection with 422 `avatar must be square`
attributed to the Avatar upload page (version 4 or its edit date) and
centre-cropping attributed to MEDIA-88, and names who settles it; and
`## Sources` lists both documents with their version or updated date.
FAIL if the size limit is given a number anywhere as if decided, the
non-square conflict is resolved in favour of one source without the
question, either reading is missing or lacks its source, an open question
names no person or evidence that settles it, or a source is listed without
a version or date.
