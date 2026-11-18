# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories on the belevtsev/kitsu repository (Security tab, "Report a vulnerability"). Do not open a public issue or pull request for a security problem.

Please include the affected version, steps to reproduce, and the impact you expect. This is a single-maintainer project, so there are no response-time guarantees.

## Scope

In scope: the `kitsu` binary and its commands, the git hooks it installs, and the plugin files in `.claude-plugin/`, `.codex-plugin/`, `.agents/plugins/`, `hooks/`, `agents/` and `skills/`.

Out of scope: vulnerabilities in git, in third-party tools, in model providers, or in Codex and Claude Code themselves; report those upstream.

## A guardrail, not a sandbox

The gate stops a commit or a push whose content has no passing run of every declared check. It is built for people and agents that would otherwise skip a step. It is not a defence against a process that sets out to defeat it:

- `kitsu` and the agent run as the same user. Anything that user can write, the agent can write: the hooks, the records under the git directory, the git configuration and the refs.
- `git commit --no-verify` and `git push --no-verify` skip git's hooks. The plugin's guard asks before an agent runs a git call it recognises as skipping the hooks, `kitsu uninstall` or `kitsu init --observe`, and before an edit, with an edit tool or a patch, of `kitsu.toml`, a git hook (also under `core.hooksPath`), `.git/config` or the records of `kitsu`. It also reads the commands behind `env`, `sudo`, `timeout`, `exec`, `nohup`, `eval` and `sh -c`, and the `PowerShell` tool of Claude Code. It misses, on purpose, a shell command that writes those files itself (a redirection, `sed -i`, an editor), a file of another repository named by an absolute path, and input sent to a running program (`write_stdin` on Codex). It does not expand variables or aliases, and nothing asks a person. The guard is a reminder for an agent; the git hooks decide ([operations](docs/operations.md#kitsu-guard)).
- A check runs with the user's rights and can do whatever its program does.
- CI runs what the pushed `kitsu.toml` declares, so a commit that removes or weakens a check passes CI as well. The commit hook prints a notice that names each check that is removed or changed. Protect `kitsu.toml` by review, for example with code owners.

None of these is a vulnerability. Running the same checks in CI catches a commit that went around the hooks; it does not catch a weakened `kitsu.toml`.

## Network access and local records

The gate checks repository content locally. Kitsu sets `GIT_NO_LAZY_FETCH=1` on Git commands and reports missing objects in partial clones. Explicit publication confirmation uses Git to read the configured push destination and uses the authenticated GitHub CLI when a PR is requested. Starting an interactive coding host, consulting an enabled adviser or running a repository check can also contact external services.

Daily task records in the private Git directory include the developer's request, recorded results and review findings. Token records contain counts and names. Kitsu creates logs, records and decision files with mode `0600` and gate store directories with mode `0700`.

## Supported versions

Security fixes are made on `main`, and only the newest published release receives them.
Kitsu versioning starts at `v0.0.1` after the 17 November 2026 release reset. Use the
[installer](docs/release-install.md) to install its current default version.

Previously downloaded `cw` releases are not maintained. For an unreleased fix, use a successful
[CI artifact](docs/release-install.md#install-a-ci-artifact-before-publication) or
[build from source](docs/development.md#build-from-source).
