# Existing behavior

DefaultLimit is 100; HardLimit is 500. The synchronous handler owns admission.

# Decisions

Retain explicit tenant limits and the existing HTTP 400 JSON error contract.
