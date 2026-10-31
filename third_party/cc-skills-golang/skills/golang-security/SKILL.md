---
name: golang-security
description: Review or fix Go security-sensitive paths involving untrusted input, authentication, cryptography, secrets, or reachable dependency vulnerabilities.
license: MIT
metadata:
  author: samber
  version: 1.2.0
  openclaw:
    emoji: 🔒
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - govulncheck
    install:
    - kind: go
      package: golang.org/x/vuln/cmd/govulncheck@latest
      bins:
      - govulncheck
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch Bash(govulncheck:*) WebSearch AskUserQuestion EnterWorktree ExitWorktree
---

# Go security-sensitive changes

Review the requested trust boundary or changed risk path. Identify attacker-controlled data, authenticated identity, the authority required for the operation, and the protected resource. Authentication alone does not establish ownership or authorization.

Trace the path through callers and deployed controls before reporting a finding. Distinguish an exploitable defect from an optional defense-in-depth improvement, give the concrete trigger and impact, and avoid inventing a finding from a suspicious snippet alone. A read-only review does not add code comments or apply fixes.

Preserve verified trust and identity checks, constrained input handling, secret ownership, and fail-closed authorization. Avoid unsafe deserialization, injection, traversal, insecure TLS, leaked credentials, and retry paths that bypass the original check. Keep security-required CI gates intact.

Read the relevant domain: [cryptography](references/cryptography.md), [injection](references/injection.md), [filesystem](references/filesystem.md), [network](references/network.md), [cookies](references/cookies.md), [secrets](references/secrets.md), [logging](references/logging.md), [memory](references/memory-safety.md), or [third-party data](references/third-party.md). Use [architecture](references/architecture.md), [threat modeling](references/threat-modeling.md), or the [full checklist](references/checklist.md) for an explicit or necessary broader assessment.

[Tool and example guidance](references/practices.md) retains static-analysis and vulnerability commands. Choose tools for the claim at issue and the permitted environment; loading this skill does not start a full audit, fuzzing campaign, or external mutation.
