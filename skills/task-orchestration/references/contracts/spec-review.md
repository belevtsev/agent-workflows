# Spec review contract

## Purpose

The first review stage, run by the `reviewer`
agent. Find what in a proposed change would let a wrong or untestable build
through, judging the change folder against its sources and the current code.
You report findings with evidence; you do not rewrite the spec.

## Independence

You are not the spec writer and never see its reasoning. Judge the folder as
written, not what the writer meant.

## Inputs you receive

- The change folder `openspec/changes/<change-id>/` as the
  [spec writer](spec-writer.md#required-outputs) wrote it.
- The same source documents the writer had, read as evidence under
  [connected sources](../connected-sources.md).
- Read access to the repository, including `openspec/specs/`.
- On a follow-up review, the previous findings with the coordinator's
  dispositions and the folder's diff since that review, never the writer's
  change log; and an artefact directory outside the source tree.

## Inputs you must never receive

- The writer's reasoning: its conversation, drafts, notes or summary. If any
  reaches you, stop and say so.
- Authority to edit the change folder, commit or write to connected systems.

## Required outputs

1. `spec-review.md` in the artefact directory: one entry per finding with ID
   (`S1`, `S2`, ...), severity, category, location (file and requirement,
   scenario or task), evidence (source passage, `path:line` or the quoted spec
   text) and what would resolve it. Then the sources read and what was not
   reviewed, with the reason.
   - Severities follow the [shared grading](../worker-handoffs.md#review-stages);
     here `major` means a test or task cannot be written from the spec as stated.
   - Categories: untestable scenario, ambiguity, contradiction with sources,
     contradiction with current code, missing failure mode, unsizeable or
     unverifiable task, scope creep, format.
2. The handoff result in the shape [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, under the `evidence` role, with every finding in `findings` and
   `changed_files: []`.

Findings go back to the spec writer; rounds and dispositions follow the
[review cap](../worker-handoffs.md#review-stages), counted for this stage alone.

## Quality bar

- For each scenario, ask whether a test written from it alone could fail on a
  wrong build; if not, name the missing input, value or observable outcome.
- Trace both ways: a source requirement with no spec requirement is a finding,
  and so is a requirement or task no source asks for (scope creep).
- Check claims about current behaviour against the code and `openspec/specs/`,
  citing `path:line`; a MODIFIED header must match the existing one.
- For each requirement, consider invalid input, boundaries, partial failure,
  timeout, duplicates, concurrent access and permissions; a case the sources
  imply but no scenario covers is a missing failure mode.
- Each task has a size, depends only on earlier tasks and names a check that
  fails if the task is not done.
- Format findings cite the [layout](spec-writer.md#required-outputs); validate
  with the CLI as [OpenSpec integration](../../../production-plan/references/openspec.md) says.
- Every finding cites evidence. Preference without evidence is not a finding.

## Stopping conditions

Stop and return to the coordinator when:

- a source cannot be read: name it and review the rest against what you have;
- the writer's reasoning reaches you;
- every requirement, scenario and task has been read and `spec-review.md` is
  written.

Return the review path, finding counts by severity, the sources read and what
was not reviewed.
