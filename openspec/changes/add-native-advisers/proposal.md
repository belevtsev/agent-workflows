# Proposal: add-native-advisers

## Why

The owner wants built-in advisers such as TypeSafe Jev and SubQ Snap, alongside the existing
configured CLIs. These models answer typed questions through an API rather than generating prose.
The accepted plan selects both providers, choice/score/yes-no support, explicit activation, and
one question per consultation.
The owner subsequently chose to keep all advisers optional and experimental, with none enabled
by default. The normal workflow requires no adviser setup or checkpoints.

## What changes

- An offline provider catalog and version 2 native definitions with environment credentials.
- A shared HTTPS System One adapter with strict typed answers and bounded single attempts.
- Version 2 score and boolean requests, with existing advice/decision requests retained.
- Portable typed records and provider-reported usage, preserving CLI fingerprints and histories.
- Experimental provider discovery, explicit opt-in instructions, and documented setup for the built-in providers.

## Sources

The owner's accepted plan and instruction to implement it authorize this scope. API contracts
were inspected in the [TypeSafe API reference](https://docs.typesafe.ai/api/system-one), its
published OpenAPI schema, and the [SubQ Decisions API](https://docs.subq.ai/endpoints/decisions-api/).
Existing adviser configuration, execution, records, access, and checkpoint code supply the
compatibility contract. The earlier configurable-advisers change remains historical evidence.

## Boundaries

No Savant/chat adapter, images, batching, automatic access grants, threshold-based approval,
retry, provider fallback, or paid live tests. Existing review and human approval rules remain.
No advisers or required adviser checkpoints are enabled by default. Credentials, catalog discovery,
and personal definitions alone enable nothing; repository role access must explicitly opt in.
Provider-reported identity and token usage are not independently verified model behavior.

## Validation status

Implementation and local automated checks are recorded in tests.md. No independent review
or live provider evaluation is claimed. No open product decisions remain.
