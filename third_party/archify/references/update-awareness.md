# Update awareness

This copy is managed by agent-workflows. Archify's automatic upstream updater is disabled in delivery, exported update helpers, and direct helper CLI invocations. It does not fetch manifests, spawn the upstream checker, or read or write update caches or reminder preferences.

`finalize` and standalone `deliver` retain the upstream unavailable update receipt shape and report `reason: "managed-by-agent-workflows"`; `noticeRequired` is false. Direct `scripts/check-update.mjs` and `scripts/delivery-update-child.mjs` invocations, `checkForUpdate`, and `setUpdatePreference` return `{ "status": "silent", "reason": "managed-by-agent-workflows" }`. Legacy snooze, ignore, and acknowledgement arguments are also no-ops.

Update Archify only through an explicit user-requested `cw update`, which validates and activates the exact shared-pack revision. Review changes and rerun the package's offline and available browser checks before updating it. `skill-release.json` preserves the upstream 3.2.0 identity and manifest URL as provenance; its URL is not called by this distribution.

Explicit digest-pinned custom brand capture remains available when requested by the user. That authoring feature is separate from package updates; see [Brand marks](brand-marks.md).
