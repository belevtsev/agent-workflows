# Why

Tenants without an explicit batch limit need a smaller default.

# What Changes

Use 50 as the default. Keep explicit configured limits unchanged.

# Impact

Accepted decision on 2026-09-18: keep the synchronous request handler and the
existing HTTP 400 JSON error. No broker or background worker is part of this change.
