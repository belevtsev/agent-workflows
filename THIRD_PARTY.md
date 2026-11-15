# Third-party provenance and notices

Upstream copyright, license texts and notices remain with their files.
Inclusion does not imply upstream endorsement.

## cw dependencies

The `cw` binary links these pinned Go dependencies; release archives carry
their notices:

| Module | Version | Retained license |
| --- | --- | --- |
| [Go runtime and standard library](https://github.com/golang/go/tree/go1.27.4) | go1.27.4 | [BSD notice](licenses/go-LICENSE) |
| [pelletier/go-toml](https://github.com/pelletier/go-toml) | v2.4.3 | [MIT notice](licenses/go-toml-LICENSE) |
| [golang.org/x/sys](https://golang.org/x/sys) | v0.50.0 | [LICENSE](licenses/module-golang.org_x_sys-LICENSE) |

## Documents

| Location | Provenance | Retained notices |
| --- | --- | --- |
| `CODE_OF_CONDUCT.md` | [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) version 2.1, from [EthicalSource/contributor_covenant](https://github.com/EthicalSource/contributor_covenant/blob/d947a0f0cd479bac3e8f1d2d617aa7f8e7fee5b4/content/version/2/1/code_of_conduct.md) commit `d947a0f0cd479bac3e8f1d2d617aa7f8e7fee5b4`; unchanged except that the enforcement contact is filled in and the site's front matter is left out | The file's Attribution section; [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) |
| `docs/diagrams/*.svg` | Drawn by archify 3.2 (MIT License; its notices are with it on the branch `workflow-v3`, in `third_party/archify/`) from the JSON spec beside each file. Each SVG embeds subsets of the font [JetBrains Mono](https://github.com/JetBrains/JetBrainsMono) in WOFF2, as served by Google Fonts | The copyright of the JetBrains Mono Project Authors and the full text of the SIL Open Font License 1.1, in the font CSS of each SVG |

The skills and tools that earlier versions vendored are not part of this
branch. Their notices are with them on the branch `workflow-v3`.
