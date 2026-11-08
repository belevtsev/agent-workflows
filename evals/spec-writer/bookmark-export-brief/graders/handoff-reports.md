---
type: llm
focus: { source: file, path: artefacts/handoff.json }
---

The repository `testdata/bookmarks` had no `openspec/` directory before the
run; the `openspec` command-line tool may or may not be installed.

PASS if the file is a JSON object with `status` and `summary`; its
`changed_files` lists `proposal.md`, `tasks.md` and
`specs/bookmark-export/spec.md` under
`testdata/bookmarks/openspec/changes/add-bookmark-export/` and no
`design.md`; the summary or `uncertainties` says that the repository had no
`openspec/` directory and that only the change folder was created (no
`openspec init`); it states either that `openspec validate
add-bookmark-export --strict` ran with its result or that it was not run
(and why); and the open question about users with more than 10,000
bookmarks appears in `uncertainties` or the summary.
FAIL if the file is not JSON, `changed_files` is missing, empty or omits a
written file, the missing `openspec/` directory is not mentioned, validation
is claimed as passed without saying it ran, or validation status is not
stated at all.
