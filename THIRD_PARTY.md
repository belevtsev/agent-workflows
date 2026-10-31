# Third-party provenance and notices

This repository includes maintained adaptations of the following material.
Upstream copyright, license texts, and asset notices remain with their files.
The repository's own notices do not replace those terms or claim ownership of
third-party material. Inclusion does not imply upstream endorsement.

| Location | Provenance | Retained notices |
| --- | --- | --- |
| `third_party/cc-skills-golang` | [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang), by Samuel Berthe; maintained entrypoints, references, and tooling | `LICENSE` (MIT) |
| `third_party/db-postgres` | [`db-postgres` in ScotterMonk/AgentAutoFlow](https://github.com/ScotterMonk/AgentAutoFlow/tree/main/.kilocode/skills/db-postgres); the skill credits [Jeffallan](https://github.com/Jeffallan) and records upstream version 1.1.0 | The skill declares MIT; the retained source-repository `LICENSE` is CC0 1.0. Both statements are retained; no single license is asserted for the whole directory. |
| `third_party/typesafe-ai` | [typesafe-ai/skills](https://github.com/typesafe-ai/skills/tree/main/skills/typesafe-ai); [official documentation](https://docs.typesafe.ai/) | `LICENSE` (MIT, copyright TypeSafe AI); the vendored distribution contains `SKILL.md`, `references/development-consultations.md`, and `LICENSE` |
| `third_party/docker-skills` | [docker/skills](https://github.com/docker/skills/tree/935fbb0ed956d4922b49c35ff9599ac1ae4212c7), tag v0.3.0, commit `935fbb0ed956d4922b49c35ff9599ac1ae4212c7`; eleven separately registered skills | Full upstream `LICENSE` (Apache-2.0); upstream attribution and each skill's license metadata |
| `third_party/archify` | [tt-a1i/archify](https://github.com/tt-a1i/archify); tag v3.2.0, commit `208acba8d412335185b2056fedf54b3405df43fb`, refreshed from a personal 3.0.1 distribution | `LICENSE` (MIT), `THIRD_PARTY_NOTICES.md`, and `assets/JetBrainsMono-OFL.txt` (SIL Open Font License 1.1) |
| `skills/security-threat-model` | Locally maintained adaptation with retained reference material; no upstream revision is asserted | `LICENSE.txt` (Apache-2.0) |

The security threat-model adaptation narrows its trigger, updates the invocation
prompt, and asks for clarification only when missing context materially affects
the requested result. The PostgreSQL adaptation resolves resources
relative to the installed skill and applies task-scoped evidence and completion
guidance. TypeSafe consultation guidance is retained with its vendored skill.

The Go testing adaptation adds an optional catalog-resolved route to this suite's
original `test-strategy` Go fuzzing reference, with a source-relative fallback.
The reference is first-party guidance; it does not replace upstream Go testing
resources or their MIT notice. TypeSafe's discovery description is shortened
locally without upgrading or changing its API/integration guidance. These are
maintained adaptations of the retained material, not new upstream revisions.

The Go bundle's upstream `allowed-tools` lists are kept under `metadata.upstream`
rather than as live frontmatter, so Claude Code does not pre-approve those tools.

The 82 original Docker skill files were verified against their exact upstream
Git blobs at the recorded commit. Local adaptations shorten discovery and UI
descriptions, quote UI strings consistently, and identify the invoked skill in
default prompts. Automatic discovery remains enabled. Skill bodies, references,
verification guidance, helpers, assets, and `skill.yaml` files remain unchanged.
The Apache license is copied in full from that same commit.

Archify retains its working diagram resources and asset notices. Its local
adaptations make discovery concise and task delivery portable within this suite;
source-only upstream tests and repository tooling are excluded from the installed
distribution. The earlier personal tree, including excluded tests, stays
fingerprinted in `personal-skill-origins.json` because skill adoption matches it.
Per-file source and import hashes, exclusions, and adaptation notes are recorded
in [the personal skill import provenance](third_party/personal-skill-imports.json).
The [Archify distribution patch record](third_party/archify/references/distribution-patches.md)
details the disabled automatic updater, external output policy, retained runtime
features, and separate asset terms.

## Optional external Graphify runtime

The independently authored `skills/graphify` instructions and guarded `cw graphify`
command use the externally installed [`graphifyy`](https://pypi.org/project/graphifyy/0.9.91/)
package, pinned to `0.9.91`. The assessment baseline is
[Graphify-Labs/graphify commit `10130e79ed9caf8f60d998073e097531eaa1a395`](https://github.com/Graphify-Labs/graphify/tree/10130e79ed9caf8f60d998073e097531eaa1a395).
Upstream declares Apache-2.0 and ships `LICENSE`, `LICENSE-MIT`, and `NOTICE`;
the externally installed package carries those notices. This repository does
not vendor Graphify code or its upstream skill instructions. Runtime provisioning
is explicit and separate from the managed skill snapshot.

## Native installer dependencies

The native installer links these pinned Go dependencies; their notices are also
bundled with release archives:

| Module | Version | Retained license |
| --- | --- | --- |
| [Go runtime and standard library](https://github.com/golang/go/tree/go1.27.1) | go1.27.1 | [BSD notice](licenses/go-LICENSE) |
| [pelletier/go-toml](https://github.com/pelletier/go-toml) | v2.2.3 | [MIT notice](licenses/go-toml-LICENSE) |
| [go-yaml/yaml](https://github.com/go-yaml/yaml) | v3.0.1 | [MIT/Apache notices](licenses/yaml-LICENSE) |
| [spf13/cobra](https://github.com/spf13/cobra) | v1.10.2 | [Apache notice](licenses/cobra-LICENSE) |
| [spf13/pflag](https://github.com/spf13/pflag) | v1.0.9 | [BSD notice](licenses/pflag-LICENSE) |
| [inconshreveable/mousetrap](https://github.com/inconshreveable/mousetrap) | v1.1.0 | [Apache notice](licenses/mousetrap-LICENSE) |
