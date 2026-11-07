# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories on the belevtsev/agent-workflows repository (Security tab, "Report a vulnerability"). Do not open a public issue or pull request for a security problem.

Please include the affected version, steps to reproduce, and the impact you expect. You can expect an acknowledgement, and a fix or explanation as time allows. This is a single-maintainer project, so there are no response-time guarantees.

## Scope

In scope:

- The `cw` binary and its subcommands.
- The installer (`install.sh`) and the files it writes into Codex and Claude Code configuration.
- Skills that run commands on the user's machine.

Out of scope: vulnerabilities in third-party tools, model providers, or Codex and Claude Code themselves; report those upstream.

## Credentials and `cw consult`

`cw consult` sends evidence excerpts to third-party adviser APIs (TypeSafe, SubQ, OpenAI). It does this only when you have saved API keys for those providers. Without saved keys, nothing is sent. Credentials are stored in `~/.config/agent-workflows`. Treat that directory as secret, and review what evidence you share before running a consultation.

## Supported versions

Only the latest release receives security fixes.
