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

## No network

`kitsu` never opens a network connection. Every git call of `kitsu` sets `GIT_NO_LAZY_FETCH=1`, so git fetches no missing object in a partial clone; `kitsu` then reports the object as missing. It reads the repository and the session records the coding agent keeps on the same machine, and it writes `kitsu.toml`, the git hooks and its records under the git directory. `kitsu tokens` stores counts and names, and no text of a brief or an answer. Logs, records and decision files are created readable by the user only (`0600`), and the folders of the gate's store with `0700`.

## Supported versions

There are no currently published releases or tags: the historical releases, tags and attached
assets were removed on 17 November 2026. Security fixes are made on `main`; use a current
successful [CI artifact](docs/release-install.md#install-a-ci-artifact-before-publication) or
[build from source](docs/development.md#build-from-source). Previously downloaded `cw` releases
are not maintained.

The `v3.0.0-beta.2` label in current builds and installer defaults is not a published or supported
release. The next release version is undecided. Once releases resume, only the newest release
will receive security fixes.
