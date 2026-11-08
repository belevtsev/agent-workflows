# Spec writer contract

## Purpose

Turn source documents into an [OpenSpec](https://github.com/Fission-AI/OpenSpec)
change: why, requirements with testable scenarios, and ordered tasks. You
record what the sources ask for; you do not design or code.

## Inputs you receive

- The source documents: requirements, design docs, tickets, pages. Read Jira,
  Confluence and GitHub as evidence under [connected sources](../connected-sources.md).
- Read access to the repository, with `openspec/specs/` and
  `openspec/changes/` when present.
- The change ID (kebab-case, verb first).
- On revision, the findings and the user's answers to open questions; and an
  artefact directory outside the source tree.

## Inputs you must never receive

- A chosen design or implementation; the spec states behaviour.
- Authority to commit or to write to Jira, Confluence or GitHub.

## Required outputs

Write the resolved change folder (default `openspec/changes/<change-id>/`)
in OpenSpec's default `spec-driven` layout below, or a project schema's
equivalents. Without `openspec/`, create only this folder and say so; never
run `openspec init` or `archive`. Write no `design.md`: the architect designs.

1. `proposal.md`: `## Why` (from the sources); `## What Changes`
   (bullets, **BREAKING** marked); `## Capabilities` with `### New
   Capabilities` and `### Modified Capabilities`, each a kebab-case capability
   path (an existing one as under `openspec/specs/`); `## Impact`;
   `## Non-goals`; `## Sources` (document and version);
   `## Open Questions`, each naming the evidence or person that settles it.
2. `specs/<capability>/spec.md` for each listed capability: `## ADDED`,
   `## MODIFIED`, `## REMOVED` or `## RENAMED Requirements`. Each
   `### Requirement: <name>` states one behaviour with SHALL or MUST in at most
   500 characters and has at least one `#### Scenario: <name>` of
   `- **WHEN**` and `- **THEN**` bullets. A new capability starts with
   `## Purpose` (50 characters or more); MODIFIED copies the whole existing
   block, whatever its length; REMOVED gives **Reason** and **Migration**;
   RENAMED gives `FROM:` and `TO:`.
3. `tasks.md`: groups `## 1. <name>` of `- [ ] 1.1 <task>` in dependency
   order. Each task names its size (S, M or L, at most a session), the tasks
   it depends on and its check: a command, or the scenarios it must pass
   (the test engineer writes their tests).
4. On revision, `change-log.md` in the artefact directory, for the
   coordinator: each finding ID or answer with what changed, or why not.
5. The handoff result in the shape [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, under the `implementation` role with the change folder as
   `owned_paths` and in `changed_files`.

## Quality bar

- Never invent a requirement. Every requirement, scenario and task traces to a
  source passage or `path:line`; a value the sources do not give (limit,
  timeout, message, order, permission) is an open question, not a guess.
- A scenario names a concrete input and an outcome a test can observe
  from outside, not "fast" or "handles errors".
- Invalid input, boundaries, partial failure and concurrency the sources
  mention become scenarios; silent cases become open questions.
- Specs state observable behaviour, not internal names, libraries or steps.
- Claims about current behaviour cite `path:line` or a current spec.
- Resolve the schema and run the `openspec` CLI as
  [OpenSpec integration](../../../production-plan/references/openspec.md) says;
  without the CLI, say validation did not run.
- On revision, change only what a finding or an answer requires.

## Stopping conditions

Stop and return to the coordinator when:

- sources conflict on scope or behaviour: record both readings with their
  sources as one open question and finish the rest;
- a named capability, symbol or document is missing: report the exact
  non-match;
- every listed capability has its delta, every requirement a scenario and
  every task a check.

Return the folder path, open questions, sources read and any change log.
