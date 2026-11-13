# Contributing

Thanks for helping improve agent-workflows. This file explains how to build, test and propose changes.

Everyone who takes part follows the [code of conduct](CODE_OF_CONDUCT.md).

## Build and test

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

Lint and check reachable vulnerabilities with the versions CI pins, for both release platforms, as CI does:

```sh
GOWORK=off go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
GOWORK=off GOOS=linux golangci-lint run ./... && GOWORK=off GOOS=darwin golangci-lint run ./...
GOWORK=off GOOS=linux govulncheck ./... && GOWORK=off GOOS=darwin govulncheck ./...
```

`GOWORK=off go test ./internal/skillcheck` checks skill metadata, Markdown links, manifests and plugin paths on its own. CI runs gofmt, vet and the tests on Linux and macOS, golangci-lint and govulncheck for the Linux and darwin builds, and validates the plugin manifests. See `docs/development.md` for details.

## Go-only automation

First-party automation must be written in Go. The repository check (`internal/skillcheck`) rejects `.sh` and `.py` files outside `skills/` and `third_party/`. If you need a helper, add it to the `cw` command, or to `cwdev` for maintainer-only evaluation.

## Diagrams

[archify](third_party/archify/SKILL.md), vendored under `third_party/archify`, is the only diagram tool; do not add diagram sources in any other format. Each diagram is a JSON spec under `docs/diagrams/` with a static SVG export next to it, which the docs embed because GitHub cannot run the interactive HTML. To change a diagram:

1. Edit the JSON spec, never the SVG.
2. Render the viewer from the repository root with archify `finalize`, passing the spec's own `diagram_type` and `meta.quality_profile`. It validates the spec and checks the rendered page in a headless browser, so it needs Node 18 or newer and Chrome, Chromium or Edge (set `ARCHIFY_CHROME` to choose the browser). `.archify/` is ignored by Git:

   ```sh
   node third_party/archify/bin/archify.mjs finalize workflow docs/diagrams/decision-flow.json .archify/decision-flow.html --quality standard
   ```

3. Open the HTML in a browser, choose **Export → SVG · Auto** and save the download over the committed SVG, here `docs/diagrams/decision-flow.svg`, ending it with the newline `.editorconfig` requires. That export carries both colour themes, so it reads in GitHub's light and dark modes.
4. Commit the spec and the SVG together.

## Proposing or changing a skill

Skills live in `skills/<name>/SKILL.md`.

- Frontmatter must include `name` and `description`.
- Keep the description short. Every listed description is loaded into context in every session, so a long one has a real cost.
- Register the skill in `skills-manifest.json`.
- A skill that should not appear in the automatic skill listing is hidden. Set `disable-model-invocation: true` in `SKILL.md` and `policy.allow_implicit_invocation: false` in `agents/openai.yaml`.
- If the skill adapts content from another source, record the source and its license in `THIRD_PARTY.md`. Do not add content whose license is unclear.
- Put long reference material in files under the skill's `references/` directory and load it only when needed, rather than in `SKILL.md`.
- Keep skills reusable across projects: a repository's commands and constraints belong in its `AGENTS.md` or development docs, and product features, incident history and operational recipes in the owning repository's docs.

For a new skill, you can open a skill proposal issue first to discuss scope.

## Adding a language pack

A pack is built into `cw`: a `packs/<id>/pack.yaml` file, plus Go code in `internal/langpack` when it needs a new evidence rule. [Packs](docs/packs.md) describes the format, what `cw` reads and the steps.

1. Open an issue with the [pack proposal template](.github/ISSUE_TEMPLATE/pack_proposal.md) before you write code. Agree on the kind and the evidence rule first. A `language` pack is selected for every user who has no `packs.json`, as soon as they update `cw`. Its rule then applies to their new tasks.
2. Follow [Add a pack](docs/packs.md#add-a-pack), including the tests it names.
3. In the CHANGELOG entry, say how a user opts out (`cw pack remove <id>`) when the pack is selected by default.

## Pull requests

- One topic per pull request.
- Pull requests stay under about 600 changed lines, unless most of the change is deletion. Split a larger change into several pull requests and say so in each description.
- Behaviour changes need tests.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md`, in Breaking, Added, Changed, Fixed or Removed: one bullet of at most two lines that says what changed for a user and links the doc with the detail; a Breaking bullet names the action a user takes. No line above the first released version may exceed 200 characters (`go test ./internal/skillcheck` checks it).
- Update docs when commands, skills or install behaviour change.
- Run the build, vet, test, lint and vulnerability commands above before requesting review.

Releases are published when a `v*` tag is pushed. Maintainers create tags; contributors do not need to.

## Built with itself

agent-workflows is developed with its own workflow. Substantial changes run as `cw` tasks through the staged flow in the [README](README.md#how-it-works): separate agents write, test and review the change, `cw` records each step in a task record, and the commit waits until `cw task verify` passes. Task records stay in the maintainer's local state directory and are not committed. Since [pull request #38](https://github.com/belevtsev/agent-workflows/pull/38), every commit written with an agent names its model in a `Co-Authored-By` trailer, and the project's single maintainer merges every pull request. Whether the workflow improves results has not been measured yet.

## License

By contributing, you agree that your contributions are licensed under the MIT License in `LICENSE`.
