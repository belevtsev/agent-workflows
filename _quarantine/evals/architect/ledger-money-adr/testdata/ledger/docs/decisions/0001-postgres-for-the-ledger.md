# 0001: Postgres for the ledger

Date: 2025-03-02

## Outcome and constraints

The ledger needs transactions across entries of one posting and point-in-time
balances. The team runs Postgres already.

## Decision

Keep ledger entries in Postgres, in the `entries` table (`schema.sql`).
Rejected: a document store, which has no multi-row transactions in our
deployment.

## Failure and compatibility

Postgres is the system of record; backups follow the platform's policy.
