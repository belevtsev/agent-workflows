# Contributing

Thanks for helping improve agent-workflows. This file explains how to build, test and propose changes.

## Build and test

```sh
GOWORK=off go build ./...
GOWORK=off go test -count=1 ./...
```

The full developer check, which CI also runs, is:

```sh
GOWORK=off go run ./cmd/cwdev check --race --snapshot --source "$PWD"
```

See `docs/development.md` for details.

## Go-only automation

First-party automation must be written in Go. The validator rejects `.sh` and `.py` files outside `skills/`, `third_party/` and `install.sh`. If you need a helper, add it to the `cw` or `cwdev` commands.

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
- Run the full developer check before requesting review.

Releases are published when a `v*` tag is pushed. Maintainers create tags; contributors do not need to.

## License

By contributing, you agree that your contributions are licensed under the MIT License in `LICENSE`.
