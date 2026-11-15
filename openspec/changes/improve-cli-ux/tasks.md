# Tasks: improve-cli-ux

Boxes record implemented behavior and observed validation. They imply no formal stage review or
human approval; the ledger remains honest about both.

## 1. Commands and help

- [x] 1.1 Implement Preferred command tree and Shared command metadata while retaining quiet aliases and guard parity. Size: M. Depends on: none. Check: registry, dispatch, alias, and guard scenarios; test. Source: `cli-ux`: Preferred command tree, Shared command metadata.
- [x] 1.2 Implement Complete safe help and Concise usage errors at all paths without setup or execution. Size: M. Depends on: 1.1. Check: root/group/leaf/alias help matrix, no-argument behavior, missing operands, and typo scenarios; test. Source: `cli-ux`: Complete safe help, Concise usage errors.
- [x] 1.3 Implement Readonly inspection and Distinct skill operations using existing readers and editors. Size: M. Depends on: 1.1. Check: full/small tasks, role/skill/adviser/check inspection, and skill mutation scenarios; test. Source: `cli-ux`: Readonly inspection, Distinct skill operations.

## 2. Preferences and interaction

- [x] 2.1 Implement Private host preferences with explicit origins, worktree isolation, and preserved legacy ingress. Size: M. Depends on: 1.1. Check: precedence, invalid values, private storage, reset, and isolation scenarios; test. Source: `cli-ux`: Private host preferences.
- [x] 2.2 Implement Explicit terminal editors with Huh v2, model/custom entry, multiple skills, contextual help, accessible mode, preview/save, and cancellation. Size: M. Depends on: 2.1, 1.3. Check: editor scope, TTY/JSON refusal, preview/save, cancellation, and concurrent-edit scenarios; test. Source: `cli-ux`: Explicit terminal editors.
- [x] 2.3 Implement Safe shell completion for Bash, Zsh, Fish, and PowerShell from shared metadata and readonly local names. Size: M. Depends on: 1.1, 2.1. Check: generated scripts, word boundaries, option values, and no-side-effect callback scenarios; test. Source: `cli-ux`: Safe shell completion.

## 3. Instructions, documentation, and checks

- [x] 3.1 Update Preserved workflow authority command references and generated host files without expanding role limits or prompt budgets. Size: S. Depends on: 2.2, 2.3. Check: generated files, guards, and generic role tests; review of authority instructions. Source: `cli-ux`: Preserved workflow authority.
- [x] 3.2 Document the preferred surface, help, host preferences, editors, skill semantics, completion, aliases, and limits; regenerate affected archify JSON/SVG pairs. Size: M. Depends on: 3.1. Check: links, anchors, command examples, diagram validation/export/visual inspection, and record mappings. Source: every requirement in `cli-ux`; design.md.
- [x] 3.3 Run required build, vet, race, lint, vulnerability, documentation, generation, diagram, and record checks; record only observed results and limits. Size: M. Depends on: 3.2. Check: actual named commands in tests.md; CONTRIBUTING.md. Source: proposal.md, Validation and limits; CONTRIBUTING.md.
