# Glossary

Each term below has one meaning across the skill. A rule lives in one place;
this page links to it instead of restating it.

## Tasks

- **Substantial task**: defined once, in the
  [working conventions](../../../global/AGENTS.md#substantial-tasks).
- **Small task**: any other task. Its rule is in the same section.
- **Open decision**: a design choice with real alternatives that the source
  cannot settle.
- **Task record**: what `cw task` writes for one task: the plan, assignments,
  handoffs, checks and consultations.
- **Profile**: `lite` (the default), `standard` or `strict`, fixed at
  `cw task start` ([profiles](../../../docs/operations.md#profiles)).
- **Stage**: one step of the [flow](contracts/README.md#flow), such as tests,
  implementation or code review, with its own worker and review.
- **Checkpoint**: the `before` and `final` consultations of the
  [decision flow](../SKILL.md#follow-the-decision-flow).
- **Verified**: `cw task verify` passed on the current source.

## Roles

One name per role. Do not call a role "the coder" or "the agent".

- **Coordinator**: the agent that loaded this skill.
- **Worker**: a fresh agent the coordinator dispatches for one stage, with one
  [contract](contracts/README.md).
- **Spec writer**: writes the spec ([contract](contracts/spec-writer.md)).
- **Architect**: writes the design and interface contract
  ([contract](contracts/architect.md)).
- **Implementer**: writes the change ([contract](contracts/implementer.md)).
- **Test engineer**: writes the behavioural tests from the contract, never
  from the code ([contract](contracts/test-engineer.md)).
- **Adversarial tester**: opt-in; writes tests meant to fail on the integrated
  code ([contract](contracts/adversarial-tester.md)).
- **Reviewer**: reviews one stage: [spec](contracts/spec-review.md),
  [design](contracts/design-review.md), [tests](contracts/test-review.md) or
  [code](contracts/code-review.md).
- **Stage author**: the worker whose output a review checks.
- **GitHub worker**: commits, pushes and opens a PR when `cw publish` does not
  fit ([publication](github-worker.md)).
- **Adviser**: an optional outside model consulted at a checkpoint
  ([consultation](consultation-helper.md)).

## Records of work

- **Assignment**: `cw task assign` records which worker takes a stage, before
  dispatch.
- **Handoff**: `cw task handoff` records what the worker returned.
- **Acceptance**: `cw task accept` records that the coordinator checked the
  handoff ([worker handoffs](worker-handoffs.md)).
- **Review cap** and **round budget**: how many reviews a stage gets and how
  many author rounds a task gets ([worker handoffs](worker-handoffs.md)).
