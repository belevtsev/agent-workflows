# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories on the belevtsev/agent-workflows repository (Security tab, "Report a vulnerability"). Do not open a public issue or pull request for a security problem.

Please include the affected version, steps to reproduce, and the impact you expect. You can expect an acknowledgement, and a fix or explanation as time allows. This is a single-maintainer project, so there are no response-time guarantees.

## Scope

In scope:

- The `cw` binary and its subcommands.
- `cw install --host codex` and `cw uninstall --host codex`, and the files they write or remove in Codex configuration and `~/.agents/skills`.
- The plugin manifests in `.claude-plugin/`, `.codex-plugin/` and `.agents/plugins/`.
- Skills that run commands on the user's machine.

Out of scope: vulnerabilities in third-party tools, model providers, or Codex and Claude Code themselves; report those upstream.

## Credentials and `cw consult`

`cw consult` sends evidence excerpts to third-party adviser APIs (TypeSafe, SubQ, OpenAI). It does this only when a provider is enabled and its key is set in the environment or saved with `cw keys set`. Without keys, nothing is sent. Saved keys are stored in `~/.config/agent-workflows` (or `$XDG_CONFIG_HOME/agent-workflows`, or `--config-dir`). Treat that directory as secret, and review what evidence you share before running a consultation.

## Supported versions

Only the latest release receives security fixes.
