---
name: go-pki-mtls
description: Design or review Go enrollment, certificate identity, mTLS authorization, and credential or trust rotation.
---

# Go PKI and mTLS

Establish the requested credential lifecycle, transport roles, authoritative identity, resource owner, and supported trust policy from source and maintained configuration. Separate enrollment authorization, issuance, possession of a private key, TLS peer authentication, application authorization, and successful application delivery. None proves the next by itself.

For enrollment or identity decisions, read [identity and enrollment](references/identity-enrollment.md). Trace authenticated caller to certificate request identity and authorized resource owner, including alternate paths and renewal. For storage, reload, rotation, or daemon recovery, read [credential lifecycle](references/credential-lifecycle.md). Keep the existing Go ownership and cancellation model; a certificate callback or reload must not orphan work or bypass validation.

Use verified chain, validity, usage, peer-name/identity, and application authorization checks appropriate to the actual policy. Inspect custom Go TLS verification callbacks and how they interact with built-in verification. Expiry, revocation, trust overlap, and rotation timing follow the system's explicit policy; do not assume universal revocation support or prescribe new runtime limits. A valid certificate for another owner must not authorize this owner's resource.

Verify the dangerous boundary with permitted repository checks: wrong identity or trust, invalid/expired credentials, partial reload, and stale work where relevant. Separate certificate publication, active configuration, observed handshake peer/certificate, authorization result, and application delivery. A file write, reload signal, readiness response, or successful process exit does not establish a fresh handshake. If observation is unavailable, report that limit.

Loading this skill does not authorize obtaining credentials, issuing or revoking certificates, changing infrastructure, restarting services, deploying, or writing to external systems. Preserve the user's action scope and avoid exposing private keys, tokens, or raw credential material in logs or artifacts.
