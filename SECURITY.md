# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories on the belevtsev/agent-workflows repository (Security tab, "Report a vulnerability"). Do not open a public issue or pull request for a security problem.

Please include the affected version, steps to reproduce, and the impact you expect. This is a single-maintainer project, so there are no response-time guarantees.

## Scope

In scope: the `cw` binary and its commands, the git hooks it installs, and the plugin files in `.claude-plugin/`, `.codex-plugin/`, `.agents/plugins/` and `hooks/`.

Out of scope: vulnerabilities in git, in third-party tools, in model providers, or in Codex and Claude Code themselves; report those upstream.

## A guardrail, not a sandbox

The gate stops a commit or a push whose content has no passing run of every declared check. It is built for people and agents that would otherwise skip a step. It is not a defence against a process that sets out to defeat it:

- `cw` and the agent run as the same user. Anything that user can write, the agent can write: the hooks, the records under the git directory, the git configuration and the refs.
- `git commit --no-verify` and `git push --no-verify` skip git's hooks. The plugin asks before an agent runs a command that holds `--no-verify`; nothing asks a person.
- A check runs with the user's rights and can do whatever its program does.

None of these is a vulnerability. Run the same checks in CI as the backstop.

## No network

`cw` never opens a network connection. It reads and writes the repository, its git directory and the session records of the coding agent on the same machine.

## Supported versions

No release of this branch exists yet. When one does, only the newest release receives security fixes.
