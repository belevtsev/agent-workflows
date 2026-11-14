---
name: Pack proposal
about: Propose a language or domain pack for cw task verify
labels: enhancement
---

## Language or domain

Which language or domain, and the pack id you propose (lowercase, for example `python`).

## Kind

`language` (selected by default for every user without `packs.json`) or `domain` (applies only after `cw pack add`). Why?

## Detection

Which file names at the top of a project mark it, for example `pyproject.toml`.

## Evidence rule

What must a changed task have run before `cw task verify` passes? Choose one:

- the fallback rule only (at least one passed check);
- a rule `cw` already has;
- a new rule: which runs count, which do not, and how changed files map to the runs that cover them.

## Evidence

A task where the fallback rule let an incomplete change through, or another reason the pack is needed.

See [packs](https://github.com/belevtsev/agent-workflows/blob/main/docs/packs.md) for the format and what `cw` reads today.
