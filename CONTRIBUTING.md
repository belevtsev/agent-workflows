# Contributing

Thanks for helping improve `gatecub`. This file explains how to build, test and propose changes.

Everyone who takes part follows the [code of conduct](CODE_OF_CONDUCT.md).

## Build and test

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

Lint and check reachable vulnerabilities with the versions CI pins, for the three release platforms, as CI does:

```sh
GOWORK=off go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
GOWORK=off golangci-lint run ./... && GOWORK=off GOOS=darwin golangci-lint run ./... && GOWORK=off GOOS=windows golangci-lint run ./...
GOWORK=off govulncheck ./... && GOWORK=off GOOS=darwin govulncheck ./... && GOWORK=off GOOS=windows govulncheck ./...
```

`GOWORK=off go test ./internal/repocheck` checks the Markdown links, the anchors and the changelog on its own. CI runs gofmt, vet and the tests on Linux, macOS and Windows, golangci-lint and govulncheck, and validates the plugin manifests. See [development](docs/development.md) for details.

## Go-only automation

First-party automation is written in Go. If you need a helper, add it to the `gatecub` command or to a test.

## Diagrams

archify is the only diagram tool; do not add diagram sources in any other format, and no Mermaid. It is not part of this repository: get it from the branch `workflow-v3` (`third_party/archify`). Each diagram is a JSON spec under `docs/diagrams/` with a static SVG export next to it, and a document shows the SVG as an image. To change a diagram:

1. Edit the JSON spec, never the SVG. Keep the picture narrow and its texts short: GitHub shows a README about 830 pixels wide and scales a wider picture down. The diagrams here are at most 832 pixels wide. A box of the default width holds 17 characters of small text; archify shrinks a longer one, and the `width` of a node gives it room. Give every type of box that the spec uses a label under `meta.legend.entries`: the legend then says what a colour means in this picture, and the test below can compare it.
2. Render the viewer page with archify `finalize`, passing the spec's own `diagram_type` and `meta.quality_profile`. The diagrams here use `showcase`, the stricter profile, which also refuses arrows that cross. `finalize` validates the spec and checks the page in a headless browser, so it needs Chrome, and Node 18 or newer; the diagrams here were rendered with Bun 1.3 in place of Node. git ignores `.archify/`.

   ```sh
   git worktree add --detach ../archify origin/workflow-v3
   node ../archify/third_party/archify/bin/archify.mjs finalize workflow docs/diagrams/gate.json .archify/gate.html --quality showcase
   ```

   `docs/diagrams/overview.json` is an `architecture`, the other two are a `workflow`.

3. Open the page in a browser, choose **Export → SVG · Auto** and save the download over the committed SVG, here `docs/diagrams/gate.svg`, ending it with a newline. That export carries both colour themes and its own font. Look at it in the light theme and in the dark one.
4. Show it in a document with `<img src="..." alt="..." width="100%">`, so that it fills the width of the page, and say in `alt` what the picture says.
5. Commit the spec and the SVG together. A test in `internal/repocheck` fails when a spec has no SVG, when no document shows the SVG as an image, and when the SVG is not the export of its spec as far as an SVG records that: the title, the lanes, the groups, the phases, the boundaries, the legend, and every box and arrow with its texts, its type or variant, and its icon or role. It cannot see a box that only moved, a size, the route of an arrow or the variant of a group: after such a change, your look at the picture is the check.

## Pull requests

- One topic per pull request.
- Pull requests stay under about 600 changed lines, unless most of the change is deletion. Split a larger change into several pull requests and say so in each description.
- Behaviour changes need tests.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md`, in Breaking, Added, Changed, Fixed or Removed: one bullet of at most two lines that says what changed for a user and links the doc with the detail; a Breaking bullet names the action a user takes. No line above the first released version may exceed 200 characters (`go test ./internal/repocheck` checks it).
- Update docs when commands or install behaviour change.
- Run the build, vet, test, lint and vulnerability commands above before requesting review.

Releases are published when a `v*` tag is pushed. Maintainers create tags; contributors do not need to.

## Built with itself

`gatecub.toml` declares this repository's checks: build, vet, test, lint, and `gatecub change check` on the task records under `openspec/changes/`. Run `gatecub init` in your clone to install the hooks; then a commit needs a passing `gatecub run`. Every commit written with an agent names its model in a `Co-Authored-By` trailer. Whether the gate or the roles improve results has not been measured yet.

## License

By contributing, you agree that your contributions are licensed under the MIT License in `LICENSE`.
