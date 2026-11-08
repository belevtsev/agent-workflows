# link-expiry Specification

## Purpose
Short links stop redirecting after they expire, so stale or abused links do
not live forever.

## Requirements
### Requirement: Default link lifetime
A link created without an explicit expiry SHALL expire 30 days after its creation time. A request for an expired link SHALL return HTTP 410 Gone.

#### Scenario: Link requested before the default expiry
- **GIVEN** a link created at 2026-01-01T00:00:00Z without an expiry
- **WHEN** it is requested at 2026-01-30T23:59:59Z
- **THEN** the response is HTTP 302 with `Location` set to the link's target

#### Scenario: Link requested at the default expiry
- **GIVEN** a link created at 2026-01-01T00:00:00Z without an expiry
- **WHEN** it is requested at 2026-01-31T00:00:00Z
- **THEN** the response is HTTP 410 Gone

### Requirement: Explicit expiry
A link created with an `expires_at` timestamp SHALL expire at exactly that instant instead of after the default lifetime.

#### Scenario: Link with an explicit expiry
- **GIVEN** a link created at 2026-01-01T00:00:00Z with `expires_at` 2026-01-02T12:00:00Z
- **WHEN** it is requested at 2026-01-02T12:00:00Z
- **THEN** the response is HTTP 410 Gone
