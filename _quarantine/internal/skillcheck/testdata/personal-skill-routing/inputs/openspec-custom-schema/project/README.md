# Batch API

The synchronous request handler processes one batch at a time. A configured
batch limit is resolved before admission. A limit above HardLimit receives the
existing HTTP 400 JSON error. Tenants with no configured value use DefaultLimit.

The service supports rolling upgrades. No source or executed check supplied in
this snapshot establishes the currently deployed revision.
