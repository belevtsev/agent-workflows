# Contributing

Thanks for helping improve agent-workflows. This file explains how to build, test and propose changes.

## Build and test

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

`GOWORK=off go test ./internal/skillcheck` checks skill metadata, Markdown links, manifests and plugin paths on its own. CI runs gofmt, vet and the tests on Linux and macOS, and validates the plugin manifests. See `docs/development.md` for details.

## Go-only automation

First-party automation must be written in Go. The repository check (`internal/skillcheck`) rejects `.sh` and `.py` files outside `skills/` and `third_party/`. If you need a helper, add it to the `cw` command, or to `cwdev` for maintainer-only evaluation.

## Diagrams

[archify](third_party/archify/SKILL.md), vendored under `third_party/archify`, is the only diagram tool; do not add diagram sources in any other format. Each diagram is a JSON spec under `docs/diagrams/` with a static SVG export next to it, which the docs embed because GitHub cannot run the interactive HTML. Edit the spec, never the SVG, then regenerate the SVG from the repository root (Node 18 or newer and Chrome):

```sh
node third_party/archify/bin/archify.mjs finalize workflow docs/diagrams/workflow-overview.json /tmp/workflow-overview.html --quality showcase
```

`finalize` validates the spec and checks the rendered page in a headless browser. Open the HTML it wrote, choose **Export → SVG · Auto** and save the file over `docs/diagrams/workflow-overview.svg`; that export carries both colour themes, so it reads in GitHub's light and dark modes. Pass the spec's own `meta.quality_profile` to `--quality` (`decision-flow.json` uses `standard`). Keep the HTML out of the repository.

## Proposing or changing a skill

Skills live in `skills/<name>/SKILL.md`.

- Frontmatter must include `name` and `description`.
- Keep the description short. Every listed description is loaded into context in every session, so a long one has a real cost.
- Register the skill in `skills-manifest.json`.
- A skill that should not appear in the automatic skill listing is hidden. Set `disable-model-invocation: true` in `SKILL.md` and `policy.allow_implicit_invocation: false` in `agents/openai.yaml`.
- If the skill adapts content from another source, record the source and its license in `THIRD_PARTY.md`. Do not add content whose license is unclear.
- Put long reference material in files under the skill's `references/` directory and load it only when needed, rather than in `SKILL.md`.

For a new skill, you can open a skill proposal issue first to discuss scope.

## Pull requests

- One topic per pull request.
- Behaviour changes need tests.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md`.
- Update docs when commands, skills or install behaviour change.
- Run the build, vet and test commands above before requesting review.

Releases are published when a `v*` tag is pushed. Maintainers create tags; contributors do not need to.

## License

By contributing, you agree that your contributions are licensed under the MIT License in `LICENSE`.
