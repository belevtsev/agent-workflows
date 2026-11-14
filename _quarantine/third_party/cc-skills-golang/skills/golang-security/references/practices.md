# Go Security

Read the section needed for the current task. Commands are examples to adapt to the repository's permitted environment and selected toolchain; setup, broad audits, and external actions require their own task scope.


## Overview

Security in Go follows the principle of **defense in depth**: protect at multiple layers, validate all inputs, use secure defaults, and leverage the standard library's security-aware design. Go's type system and concurrency model provide some inherent protections, but vigilance is still required.

## Security Thinking Model

Before writing or reviewing code, ask three questions:

1. **What are the trust boundaries?** — Where does untrusted data enter the system? (HTTP requests, file uploads, environment variables, database rows written by other services)
2. **What can an attacker control?** — Which inputs flow into sensitive operations? (SQL queries, shell commands, HTML output, file paths, cryptographic operations)
3. **What is the blast radius?** — If this defense fails, what's the worst outcome? (Data leak, RCE, privilege escalation, denial of service)

## Severity Levels

| Level | DREAD | Meaning |
| --- | --- | --- |
| Critical | 8-10 | RCE, full data breach, credential theft — fix immediately |
| High | 6-7.9 | Auth bypass, significant data exposure, broken crypto — fix in current sprint |
| Medium | 4-5.9 | Limited exposure, session issues, defense weakening — fix in next sprint |
| Low | 1-3.9 | Minor info disclosure, best-practice deviations — fix opportunistically |

Levels align with [DREAD scoring](threat-modeling.md).

## Research Before Reporting

Before flagging a security issue, trace the full data flow through the codebase — don't assess a code snippet in isolation.

1. **Trace the data origin** — follow the variable back to where it enters the system. Is it user input, a hardcoded constant, or an internal-only value?
2. **Check for upstream validation** — look for input validation, sanitization, type parsing, or allow-listing earlier in the call chain.
3. **Examine the trust boundary** — if the data never crosses a trust boundary (e.g., internal service-to-service with mTLS), the risk profile is different.
4. **Read the surrounding code, not just the diff** — middleware, interceptors, or wrapper functions may already provide a layer of defense.

Account for reachable inputs and existing upstream guarantees when deciding whether there is a defect and how severe it is. Label optional defense-in-depth changes separately from exploitable findings.

For a review-only task, explain relevant evidence in the review result. Add source comments or changes only when editing is in scope and the comment conveys a durable constraint.

## Threat Modeling (STRIDE)

Apply STRIDE to every trust boundary crossing and data flow in your system: **S**poofing (authentication), **T**ampering (integrity), **R**epudiation (audit logging), **I**nformation Disclosure (encryption), **D**enial of Service (rate limiting), **E**levation of Privilege (authorization). Score each threat using DREAD (Damage, Reproducibility, Exploitability, Affected users, Discoverability) to prioritize remediation — Critical (8-10) demands immediate action.

For the full methodology with Go examples, DFD trust boundaries, DREAD scoring, and OWASP Top 10 mapping, see **[Threat Modeling Guide](threat-modeling.md)**.

## Quick Reference

| Severity | Vulnerability | Defense | Standard Library Solution |
| --- | --- | --- | --- |
| Critical | SQL Injection | Parameterized queries separate data from code | `database/sql` with `?` placeholders |
| Critical | Command Injection | Pass args separately, never via shell concatenation | `exec.Command` with separate args |
| High | XSS | Auto-escaping renders user data as text, not HTML/JS | `html/template`, `text/template` |
| High | Path Traversal | Scope untrusted file access to an allowed root | Go 1.24+: use `os.Root`. Pre-Go 1.24: use `filepath.IsLocal` + `filepath.Rel` + separator-aware checks; never rely on `filepath.Clean` + `strings.HasPrefix` alone. |
| Medium | Timing Attacks | Constant-time comparison avoids byte-by-byte leaks | `crypto/subtle.ConstantTimeCompare` |
| High | Crypto Issues | Use vetted algorithms; never roll your own | `crypto/aes`, `crypto/rand` |
| Medium | HTTP Security | TLS + security headers prevent downgrade attacks | `net/http`, configure TLSConfig |
| Low | Missing Headers | HSTS, CSP, X-Frame-Options prevent browser attacks | Security headers middleware |
| Medium | Rate Limiting | Rate limits prevent brute-force and resource exhaustion | `golang.org/x/time/rate`, server timeouts |
| High | Race Conditions | Protect shared state to prevent data corruption | `sync.Mutex`, channels, avoid shared state |

## Detailed Categories

For complete examples, code snippets, and CWE mappings, see:

- **[Cryptography](cryptography.md)** — Algorithms, key derivation, TLS configuration.
- **[Injection Vulnerabilities](injection.md)** — SQL, command, template injection, XSS, SSRF.
- **[Filesystem Security](filesystem.md)** — Path traversal, zip bombs, file permissions, symlinks.
- **[Network/Web Security](network.md)** — SSRF, open redirects, HTTP headers, timing attacks, session fixation.
- **[Cookie Security](cookies.md)** — Secure, HttpOnly, SameSite flags.
- **[Third-Party Data Leaks](third-party.md)** — Analytics privacy risks, GDPR/CCPA compliance.
- **[Memory Safety](memory-safety.md)** — Integer overflow, memory aliasing, `unsafe` usage.
- **[Secrets Management](secrets.md)** — Hardcoded credentials, env vars, secret managers.
- **[Logging Security](logging.md)** — PII in logs, log injection, sanitization.
- **[Threat Modeling Guide](threat-modeling.md)** — STRIDE, DREAD scoring, trust boundaries, OWASP Top 10.
- **[Security Architecture](architecture.md)** — Defense-in-depth, Zero Trust, auth patterns, rate limiting, anti-patterns.

## Code Review Checklist

For the full security review checklist organized by domain (input handling, database, crypto, web, auth, errors, dependencies, concurrency), see **[Security Review Checklist](checklist.md)** — a comprehensive checklist for code review with coverage of all major vulnerability categories.

## Tooling & Verification

Select tools for the changed risk and the repository's required gates. The following commands are options, not a sequence to run on every security-sensitive edit.

### Static Analysis & Linting

Security-relevant linters: `bodyclose`, `sqlclosecheck`, `nilerr`, `errcheck`, `govet`, `staticcheck`. See the `samber/cc-skills-golang@golang-lint` skill for configuration and usage.

For deeper security-specific analysis:

```bash
# Go security checker (SAST)
go get -tool github.com/securego/gosec/v2/cmd/gosec@latest
go tool gosec ./...

# Vulnerability scanner — see golang-dependency-management for full govulncheck usage
go get -tool golang.org/x/vuln/cmd/govulncheck@latest
go tool govulncheck ./...
```

To check the known CVEs of a specific module or version without scanning the whole tree (e.g. when vetting a dependency on pkg.go.dev), → See `samber/cc-skills-golang@golang-pkg-go-dev` skill.

### Security Testing

```bash
# Race detector
go test -race ./...

# Fuzz testing
go test -fuzz=Fuzz
```

## Common Mistakes

| Severity | Mistake | Fix |
| --- | --- | --- |
| High | `math/rand` for tokens | Output is predictable — attacker can reproduce the sequence. Use `crypto/rand` |
| Critical | SQL string concatenation | Attacker can modify query logic. Parameterized queries keep data and code separate |
| Critical | `exec.Command("bash -c")` | Shell interprets metacharacters (`;`, `\|`, `` ` ``). Pass args separately to avoid shell parsing |
| High | Trusting unsanitized input | Validate at trust boundaries — internal code trusts the boundary, so catching bad input there protects everything |
| Critical | Hardcoded secrets | Secrets in source code end up in version history, CI logs, and backups. Use env vars or secret managers |
| Medium | Comparing secrets with `==` | `==` short-circuits on first differing byte, leaking timing info. Use `crypto/subtle.ConstantTimeCompare` |
| Medium | Returning detailed errors | Stack traces and DB errors help attackers map your system. Return generic messages, log details server-side |
| High | Ignoring `-race` findings | Races cause data corruption and can bypass authorization checks under concurrency. Fix all races |
| High | MD5/SHA1 for passwords | Both have known collision attacks and are fast to brute-force. Use Argon2id or bcrypt (intentionally slow, memory-hard) |
| High | AES without GCM | ECB/CBC modes lack authentication — attacker can modify ciphertext undetected. GCM provides encrypt+authenticate |
| Medium | Binding to 0.0.0.0 | Exposes service to all network interfaces. Bind to specific interface to limit attack surface |

## Security Anti-Patterns

| Severity | Anti-Pattern | Why It Fails | Fix |
| --- | --- | --- | --- |
| High | Security through obscurity | Hidden URLs are discoverable via fuzzing, logs, or source | Authentication + authorization on all endpoints |
| High | Trusting client headers | `X-Forwarded-For`, `X-Is-Admin` are trivially forged | Server-side identity verification |
| High | Client-side authorization | JavaScript checks are bypassed by any HTTP client | Server-side permission checks on every handler |
| High | Shared secrets across envs | Staging breach compromises production | Per-environment secrets via secret manager |
| Critical | Ignoring crypto errors | `_, _ = encrypt(data)` silently proceeds unencrypted | Always check errors — fail closed, never open |
| Critical | Rolling your own crypto | Custom encryption hasn't been analyzed by cryptographers | Use `crypto/aes` GCM, `golang.org/x/crypto/argon2` |

See **[Security Architecture](architecture.md)** for detailed anti-patterns with Go code examples.

## Additional Resources

- [Go Security Best Practices](https://go.dev/doc/security/best-practices)
- [gosec Security Linter](https://github.com/securego/gosec)
- [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)
- [OWASP Go Secure Coding Practices](https://owasp.org/www-project-go-secure-coding-practices-guide/)
