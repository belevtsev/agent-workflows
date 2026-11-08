---
type: llm
focus: { source: file, path: artefacts/handoff.json }
---

PASS if the file is a JSON object whose `changed_files` lists the files
written under `testdata/profiles/openspec/changes/add-avatar-upload/`
(at least `proposal.md`, `tasks.md` and `specs/avatar-upload/spec.md`), and
whose `uncertainties` (or summary) carries both open questions: the
unstated upload size limit, and the conflict over non-square images between
the Avatar upload page (reject with 422) and MEDIA-88 (centre-crop), with
each reading tied to its source.
FAIL if the file is not JSON, `changed_files` is missing or omits a written
file, either open question is absent, or the conflict is reported as
resolved.
