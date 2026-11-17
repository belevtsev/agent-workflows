# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Historical calibration | Pin actual public stdout/contracts and independent per-behavior labels; report verdicts by split | Existing retained charset evidence and offline parsing | yes | Reuse public source bytes; preserve original graders, scores and frozen/private assets |
| Optional receipt | Write a new offline calibration result without overwriting existing evidence | Calibration result and Go test option | yes | Test-only output; no new CLI or live harness path |
| Generated context checks | Reconstruct source facts from bounded read/search pages; test Git changes, freshness, metadata and cursor failures | Existing context collector and cache | yes | Direct source and Git observations supply an independent comparison |
| Protocol/state checks | Preserve accepted worker claims and reconstruct bounded delta projections | Existing return ingestion and coordinator snapshots | yes | Reuse local Git fixtures and fake checks; retain existing authority boundaries |
| Native Go fuzz targets | Mutate generated evidence and malformed requests within bounded local campaigns | Deterministic seed corpus and equivalence or rejection assertions | yes | Standard Go fuzzing and failing-corpus replay; no model judge |

## Calibration boundary

The fixture pins all 44 observations from four retained public charset stdout
files. Baseline/reference form development (four defects, 18 controls);
candidate-1/candidate-2 form validation (eight defects, 14 controls).
The split is fixed public retrospective validation with overlapping behaviors
from the same task/repository; candidate-1 is identical to the reference. It
cannot support unseen, blind-holdout or general model-quality claims.

Each correct control describes one observed behavior, not a fully correct
program. Expanded deterministic checks are evaluated against the individual
labels. Original-coverage replay identifies labeled defects beyond original
task/control coverage, without running the historical grader or changing its
frozen scores. Missing, malformed, mismatched or unexecuted evidence is unknown
or rejected, never promoted to a passing behavior or detected defect.

## Equivalence boundary

Context tests compare fully recovered required evidence with direct source
observations, counting every page needed for the comparison. Cursor and budget
checks must not silently lose required metadata or accept another item's
continuation. Freshness cases change underlying files before re-observation.
Worker-return properties compare claims while preserving separation from
observed local facts and authority. State properties compare bounded
projections with current snapshots after transitions, including cleared state.

Ordinary tests replay fixed generated cases and native fuzz seeds. Explicit
bounded fuzz campaigns mutate additional inputs; a passing campaign is not
exhaustive equivalence. Go emits a failing corpus entry for focused replay.
No persistent daemon, routing policy or protocol/schema change is introduced.

## Rules

| Rule | Checked by |
| --- | --- |
| Calibration reads actual retained bytes with pinned hashes and independent labels | test, review |
| Development/validation membership is fixed and reported as public retrospective evidence | test, review |
| Defects, correct behavior controls, false alarms and unknowns remain distinct | test |
| Malformed or unavailable evidence cannot manufacture passing or defect authority | test |
| Expansion, freshness, bindings, bounds and state clearing remain observable | test |
| Worker claims and bounded state views grant no review/check/completion authority | test, review |
| No model is invoked, no paid pilot is launched and historical assets remain unchanged | test, review |
| Compact opt-in, roles, models, routing and original worker inputs remain unchanged | test, review |

The existing flow and authority are unchanged, so no diagram update is needed.
The former paid comparison retains its original reported result and limits.
