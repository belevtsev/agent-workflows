# Third-party provenance and notices

This repository includes maintained adaptations of the following material.
Upstream copyright, license texts, and asset notices remain with their files.
The repository's own notices do not replace those terms or claim ownership of
third-party material. Inclusion does not imply upstream endorsement.

| Location | Provenance | Retained notices |
| --- | --- | --- |
| `third_party/cc-skills-golang` | [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang), by Samuel Berthe; maintained entrypoints, references, and tooling | `LICENSE` (MIT) |
| `third_party/typesafe-ai` | [typesafe-ai/skills](https://github.com/typesafe-ai/skills/tree/main/skills/typesafe-ai); [official documentation](https://docs.typesafe.ai/) | `LICENSE` (MIT, copyright TypeSafe AI); the vendored distribution contains `SKILL.md`, `references/development-consultations.md`, and `LICENSE` |
| `third_party/docker-skills` | [docker/skills](https://github.com/docker/skills/tree/935fbb0ed956d4922b49c35ff9599ac1ae4212c7), tag v0.3.0, commit `935fbb0ed956d4922b49c35ff9599ac1ae4212c7`; eleven separately registered skills | Full upstream `LICENSE` (Apache-2.0); upstream attribution and each skill's license metadata |
| `third_party/archify` | [tt-a1i/archify](https://github.com/tt-a1i/archify); tag v3.2.0, commit `208acba8d412335185b2056fedf54b3405df43fb`, refreshed from a personal 3.0.1 distribution | `LICENSE` (MIT), `THIRD_PARTY_NOTICES.md`, and `assets/JetBrainsMono-OFL.txt` (SIL Open Font License 1.1) |
| `skills/codebase-design` | [`codebase-design` in mattpocock/skills](https://github.com/mattpocock/skills/tree/4ae2317ddb22b046e26455e4f9c9b3aaaf62212d/skills/engineering/codebase-design) and [`module-depth` in auralshin/coding-skills](https://github.com/auralshin/coding-skills/tree/35766f5523b532a7d4dbfaa951fd74451abda7f0/skills/module-depth); merged and rewritten for Go | `LICENSE` (MIT) |
| `skills/domain-modeling` | [`domain-modeling` in mattpocock/skills](https://github.com/mattpocock/skills/tree/4ae2317ddb22b046e26455e4f9c9b3aaaf62212d/skills/engineering/domain-modeling); edited for this suite | `LICENSE` (MIT) |
| `skills/parse-dont-validate` | [`parse-dont-validate` in auralshin/coding-skills](https://github.com/auralshin/coding-skills/tree/35766f5523b532a7d4dbfaa951fd74451abda7f0/skills/parse-dont-validate) and [`type-design-analyzer` in anthropics/claude-plugins-official](https://github.com/anthropics/claude-plugins-official/blob/a7ba5b93db0c7c8d17f3f1d25853adca79598a1a/plugins/pr-review-toolkit/agents/type-design-analyzer.md); merged and rewritten for Go | `LICENSE` (MIT, Apache-2.0) |
| `skills/test-strategy` references | [`writing-good-tests.md` in obra/superpowers](https://github.com/obra/superpowers/blob/2ce8aa15d05bd57213e6f746f953aafb7ded7a8b/skills/test-driven-development/writing-good-tests.md) and [`metamorphic-testing` in auralshin/coding-skills](https://github.com/auralshin/coding-skills/tree/35766f5523b532a7d4dbfaa951fd74451abda7f0/skills/metamorphic-testing), rewritten for Go. The property and mutation guidance restates ideas from [trailofbits/skills](https://github.com/trailofbits/skills/tree/2be6433b39e009cd2b6fd3be1895f6b1faa5af9a) (CC-BY-SA-4.0) in new text, with no text reproduced | `LICENSE` (MIT) |
| Adapted references in `third_party/cc-skills-golang` | Files marked as adapted carry material from auralshin/coding-skills, mattpocock/skills, addyosmani/agent-skills, and anthropics/claude-plugins-official, rewritten for Go | `ADAPTED-LICENSES` (MIT, Apache-2.0) |
| `skills/expand-contract`, `skills/isolation-anomalies`, `skills/retry-amplification` | [`expand-contract`, `isolation-anomalies`, and `retry-amplification` in auralshin/coding-skills](https://github.com/auralshin/coding-skills/tree/35766f5523b532a7d4dbfaa951fd74451abda7f0/skills), rewritten for Go and corrected against PostgreSQL 17 | `LICENSE` in each skill (MIT) |
| Adapted references in `skills/` | References marked as adapted in `go-principal-engineer`, `software-architecture`, `protobuf-contracts`, and `code-review` carry material from auralshin/coding-skills, mattpocock/skills, addyosmani/agent-skills, and anthropics/claude-plugins-official, edited for this suite | `licenses/adapted-skill-sources-LICENSE` (MIT, Apache-2.0) |
| `skills/clocks-and-time` | [`clocks-and-time` in auralshin/coding-skills](https://github.com/auralshin/coding-skills/tree/35766f5523b532a7d4dbfaa951fd74451abda7f0/skills/clocks-and-time), rewritten for Go | `LICENSE` (MIT) |
| `third_party/cc-skills-golang/skills/golang-code-style/references/style-guides.md` | Restates decisions from the [Google Go style guide](https://google.github.io/styleguide/go/) (CC-BY-3.0) and the [Uber Go style guide](https://github.com/uber-go/guide) (Apache-2.0) | Credited in the file |
| `skills/security-threat-model` | Locally maintained adaptation with retained reference material; no upstream revision is asserted | `LICENSE.txt` (Apache-2.0) |

The security threat-model adaptation narrows its trigger, updates the invocation
prompt, and asks for clarification only when missing context materially affects
the requested result. The PostgreSQL adaptation resolves resources
relative to the installed skill and applies task-scoped evidence and completion
guidance, and it runs only when invoked by name. TypeSafe consultation guidance is retained with its vendored skill.

The Go testing adaptation adds an optional catalog-resolved route to this suite's
original `test-strategy` Go fuzzing reference, with a source-relative fallback.
The reference is first-party guidance; it does not replace upstream Go testing
resources or their MIT notice. TypeSafe's discovery description is shortened
locally without upgrading or changing its API/integration guidance. These are
maintained adaptations of the retained material, not new upstream revisions.

The Go bundle's upstream `allowed-tools` lists are kept under `metadata.upstream`
rather than as live frontmatter, so Claude Code does not pre-approve those tools.
Thirteen library-specific Go skills (`golang-samber-*`, `golang-uber-fx`,
`golang-uber-dig`, `golang-google-wire`, `golang-swagger`, `golang-graphql` and
`golang-spf13-viper`) add `disable-model-invocation: true` and an
`agents/openai.yaml` that disables implicit invocation, so they run only when
invoked by name.

The 82 original Docker skill files were verified against their exact upstream
Git blobs at the recorded commit. Local adaptations shorten discovery and UI
descriptions, quote UI strings consistently, and identify the invoked skill in
default prompts. All but `docker-destructive-guardrails` also disable implicit
invocation on both hosts (`disable-model-invocation: true` in `SKILL.md`,
`policy.allow_implicit_invocation: false` in `agents/openai.yaml`), so they run
only when invoked by name; the guardrails skill keeps automatic discovery. Skill bodies, references,
verification guidance, helpers, assets, and `skill.yaml` files remain unchanged.
The Apache license is copied in full from that same commit.

Archify retains its working diagram resources and asset notices. Its local
adaptations make discovery concise and task delivery portable within this suite;
source-only upstream tests and repository tooling are excluded from the installed
distribution. Per-file source and import hashes, exclusions, and adaptation notes are recorded
in [the personal skill import provenance](third_party/personal-skill-imports.json).
The [Archify distribution patch record](third_party/archify/references/distribution-patches.md)
details the disabled automatic updater, external output policy, retained runtime
features, and separate asset terms.

## Optional external Graphify runtime

The independently authored `skills/graphify` instructions and guarded `cw graphify`
command use the externally installed [`graphifyy`](https://pypi.org/project/graphifyy/0.9.103/)
package, pinned to `0.9.103`. The assessment baseline is
[Graphify-Labs/graphify commit `16218bfc80d4ca2799734d045039f7ed1a5fa9fb`](https://github.com/Graphify-Labs/graphify/tree/16218bfc80d4ca2799734d045039f7ed1a5fa9fb).
Upstream declares Apache-2.0 and ships `LICENSE`, `LICENSE-MIT`, and `NOTICE`;
the externally installed package carries those notices. This repository does
not vendor Graphify code or its upstream skill instructions. Runtime provisioning
is explicit and separate from the skills.

## cw dependencies

The `cw` binary links these pinned Go dependencies; release archives carry
their notices:

| Module | Version | Retained license |
| --- | --- | --- |
| [Go runtime and standard library](https://github.com/golang/go/tree/go1.27.4) | go1.27.4 | [BSD notice](licenses/go-LICENSE) |
| [pelletier/go-toml](https://github.com/pelletier/go-toml) | v2.2.3 | [MIT notice](licenses/go-toml-LICENSE) |
| [go-yaml/yaml](https://github.com/go-yaml/yaml) | v3.0.1 | [MIT/Apache notices](licenses/yaml-LICENSE) |
| [spf13/cobra](https://github.com/spf13/cobra) | v1.10.2 | [Apache notice](licenses/cobra-LICENSE) |
| [spf13/pflag](https://github.com/spf13/pflag) | v1.0.9 | [BSD notice](licenses/pflag-LICENSE) |
| [inconshreveable/mousetrap](https://github.com/inconshreveable/mousetrap) | v1.1.0 | [Apache notice](licenses/mousetrap-LICENSE) |
| [golang.org/x/sys](https://golang.org/x/sys) | v0.49.0 | [LICENSE](licenses/module-golang.org_x_sys-LICENSE) |
