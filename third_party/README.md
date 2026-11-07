# Third-party skills

Skills from other projects that ship with agent-workflows. Each keeps its own
license; [THIRD_PARTY.md](../THIRD_PARTY.md) records the source, version and
license of every one, and of the files first-party skills adapt.

| Folder | What | Source | How it ships |
|---|---|---|---|
| [cc-skills-golang](cc-skills-golang/) | 46 Go specialist skills | [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang), MIT | Its own plugin in this marketplace |
| [archify](archify/) | Architecture and workflow diagrams | [tt-a1i/archify](https://github.com/tt-a1i/archify), MIT | Part of the `agent-workflows` plugin |
| [typesafe-ai](typesafe-ai/) | Typed AI judgments with TypeSafe | [typesafe-ai/skills](https://github.com/typesafe-ai/skills), MIT | Part of the `agent-workflows` plugin |
| [docker-skills](docker-skills/) | Dockerfiles, Compose, Docker Agent and Sandboxes | [docker/skills](https://github.com/docker/skills), Apache-2.0 | Linked for Codex by `cw install --host codex`; Docker also publishes its own plugin |

## The Go pack is a fork

`cc-skills-golang` is maintained here, not mirrored. Every `SKILL.md` entrypoint
is rewritten: shorter descriptions for the skill listing, upstream
`allowed-tools` moved into metadata so the host does not pre-approve tools, and
rarely used library skills hidden. Everything else matches the upstream commit
recorded in [UPSTREAM](cc-skills-golang/UPSTREAM), which also says how to refresh.

## Rules for vendored content

- Record the upstream repository, version or commit, and license in
  [THIRD_PARTY.md](../THIRD_PARTY.md) before adding anything here.
- Do not add content whose license is unclear.
- Keep local changes to the entrypoints where possible, so a refresh is a diff
  against one recorded commit.
- [`personal-skill-imports.json`](personal-skill-imports.json) pins per-file
  hashes for archify and the Docker skills; a test checks the files still match.
