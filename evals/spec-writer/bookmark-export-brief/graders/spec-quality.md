---
type: llm
focus: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/specs/bookmark-export/spec.md }
---

The source is a product brief (version 3) asking for `GET
/bookmarks/export?format=csv` with: content type `text/csv` and
`Content-Disposition: attachment; filename="bookmarks.csv"`; header row
`url,title,created_at`; `created_at` in RFC 3339 UTC; rows oldest first by
`created_at`; RFC 4180 quoting of a title containing a comma or a double
quote; a header-only file for a user with no bookmarks; only the caller's own
bookmarks; the existing `401` for a request without a signed-in user; `400`
with body `unsupported format: <value>` for any other `format`, and a missing
`format` treated as `csv`; at most 10,000 bookmarks per export. The brief
does not say what happens to a user with more than 10,000 bookmarks. It
mentions `encoding/csv`, `exportHandler` and `Store.List` only as an
unagreed engineering suggestion.

PASS if the file has `## Purpose` and `## ADDED Requirements`; the
requirements cover every behaviour listed above (one behaviour per
requirement, each stated with SHALL or MUST); every requirement has at least
one `#### Scenario:` with `- **WHEN**` and `- **THEN**` bullets; every
scenario names a concrete input (for example a specific title such as
`Go, the language`, a specific `format` value, a user with zero bookmarks)
and an outcome a test can observe from outside (status code, header value or
exact body lines); the unsupported-format and unauthenticated cases each
have a scenario; and nothing asserts what happens above 10,000 bookmarks
(no truncation, status code, pagination or error message for that case).
FAIL if any listed behaviour is missing, a scenario has no concrete input or
no observable outcome (for example "the export is correct" or "errors are
handled"), the spec names `encoding/csv`, `exportHandler`, `Store.List` or
any other internal name, library or implementation step, it invents a value
the brief does not give (a rate limit, timeout, size cap, status code or
message for more than 10,000 bookmarks), or it specifies JSON or HTML export,
import, team export or emailing.
