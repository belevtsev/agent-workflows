# Managed distribution patches

This is a self-contained agent-workflows distribution of the Archify 3.2.0 skill
from `https://github.com/tt-a1i/archify`, tag `v3.2.0`, commit
`208acba8d412335185b2056fedf54b3405df43fb`. It was refreshed from an earlier
personal 3.0.1 copy by a three-way merge of these patches onto the tagged `archify/`
directory. `skill-release.json` retains the upstream stable 3.2.0 identity and
manifest URL as provenance.

The runtime retains the upstream `archify/` directory: the root metadata and license
files, `assets`, `bin`, `brand-marks`, `delta`, `examples`, `explainer`, `locales`,
`migrations`, `recipes`, `references`, `renderers`, `schemas`, and `scripts`. All
`bin` helpers, JSON examples, locale files, and existing HTML examples are retained.
Caches, dependencies, `.git`, upstream website and release tooling, and tests needing
the upstream parent project are excluded. The shared pack's adoption catalog keeps
the complete untouched inventory of the earlier personal copy, and the import ledger
records the refreshed files' provenance.

The distribution patches are:

- `SKILL.md` has a shorter activation description, names this managed
  distribution, and requires artifacts/evidence outside the installed package.
  All diagram types, explainers, Mermaid/plain-language/JSON inputs, repository
  evidence, and everyday subject activation remain available.
- `bin/delivery-update.mjs`, `scripts/check-update.mjs`, and
  `scripts/delivery-update-child.mjs` are updater no-ops. Delivery preserves the
  unavailable receipt shape; direct helpers preserve the silent receipt shape.
  Both report `reason: "managed-by-agent-workflows"`, and the compact `finalize`
  receipt omits the unavailable update as upstream does. Options and legacy
  reminder arguments do not initiate fetches, child processes, cache reads, or
  cache writes. Update instructions now use explicit `cw update`, which
  validates and activates the exact shared-pack revision.
- `renderers/shared/output-path.mjs` rejects writes physically contained by the
  package, including existing files, symlink aliases, and nonexistent children
  under aliases. Unverifiable containment fails closed. Existing portable,
  native-path, input-alias, and extension contracts remain in place.
- `renderers/shared/atomic-output.mjs` repeats the managed containment check for
  capture, verification, and publication recovery. `bin/visual-check.mjs`
  rejects managed evidence paths before creating evidence directories.
  `bin/archify.mjs` checks demo and migration destinations before creating their
  output directories.
- `archify examples` and `scripts/render-examples.mjs` require an explicit
  external `--output-dir`. They do not refresh bundled HTML examples in place.
  The new `scripts/output-directory.mjs` applies this same policy to retained
  generators. Generator write modes require an external output directory;
  `--check` only reads the bundled generated file.
- `scripts/generate-validators.mjs` and `scripts/generate-brand-marks.mjs`
  validate output policy before loading optional Ajv or Simple Icons development
  dependencies. Missing optional dependencies are explicit failures. Runtime
  rendering requires Node 18 or newer and uses the bundled validators and mark
  data with Node built-ins; no npm installation is required.
- `package.json` removes commands referring to missing upstream parent
  scripts/docs/examples and provides standalone tests and resource commands.
  `package-lock.json` retains only Ajv, Simple Icons, and their locked generator
  dependencies. These development dependencies are optional for using the skill.
- Six portable upstream tests are retained with their dense-fixture helper:
  portable-path, path-semantics, output-path, finalize, finalize-browser, and
  update-contract, with import paths adjusted for this standalone layout. The
  browser test uses a relocated package and isolated external HOME/output
  directories. New managed-distribution tests cover updater no-ops, five core
  modes, external outputs, aliases, immutable hashes, missing Chrome,
  preview/opener helpers, custom digest-pinned brand capture, and separate
  third-party notices.

Explicit custom brand URLs with digest pinning are retained. Their requested
capture/reproduction can use the network; this feature is separate from the
disabled automatic updater. Built-in marks and ordinary diagrams remain local.

The MIT license covers Archify's own code/content. It does not supersede the
SIL OFL 1.1 font license, the separate individual brand-mark licenses, trademark
conditions, or per-mark provenance. `LICENSE`, `THIRD_PARTY_NOTICES.md`,
`assets/JetBrainsMono-OFL.txt`, the catalog, and generated brand provenance are
preserved. Do not describe all bundled assets as MIT-licensed.

A successful `finalize` requires its real browser gate to pass. Missing Chrome,
skipped browser checks, unavailable generator prerequisites, and unperformed
perceptual inspection are not passing evidence. Run browser tests only with an
available Chrome executable; no tools or dependencies are installed by this
distribution's verification workflow.
