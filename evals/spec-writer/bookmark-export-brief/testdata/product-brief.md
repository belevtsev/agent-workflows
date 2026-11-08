# Product brief: export bookmarks as CSV

Version: 3 (2026-10-12)
Owner: Dana Whitfield, Product
Engineering contact: Omar Reyes

## Problem

Users who move to another bookmarking tool, or who simply want a backup, have
no way to get their bookmarks out of the service. Support received 41 tickets
in September asking for an export, and today support agents copy rows out of
the database by hand, which is slow and has twice sent a user the wrong
person's list.

## Who it is for

Any signed-in user of the bookmarks service. Admin and team features are out
of scope for this release.

## What we want

1. A signed-in user can download all of their own bookmarks as a CSV file from
   `GET /bookmarks/export?format=csv`.
2. The response has the content type `text/csv` and the header
   `Content-Disposition: attachment; filename="bookmarks.csv"`.
3. The first line of the file is the header row `url,title,created_at`. Each
   following line is one bookmark. `created_at` is written in RFC 3339 in UTC,
   for example `2026-03-01T09:30:00Z`.
4. Rows are ordered by `created_at`, oldest first.
5. A title that contains a comma or a double quote is quoted the way RFC 4180
   describes, so the file opens correctly in spreadsheets.
6. A user with no bookmarks gets a file containing only the header row.
7. A user only ever sees their own bookmarks in the export.
8. A request without a signed-in user gets the same `401` response the rest of
   the API gives today.
9. Any `format` other than `csv` returns `400` with the body
   `unsupported format: <value>`; a missing `format` is treated as `csv`.

## Limits

An export covers at most 10,000 bookmarks. Fewer than 0.1% of users have more
than that today.

## Not in this release

- JSON or HTML export.
- Importing bookmarks.
- Exporting another user's or a team's bookmarks.
- Emailing the export.

## Engineering notes (from the kickoff, for context only)

Omar suggested writing the rows with Go's `encoding/csv` package from a new
`exportHandler` in `server.go`, streaming straight from `Store.List`. Nobody
has agreed a design yet.

## Success measure

Export-related support tickets drop below 5 a month within two months of
release.
