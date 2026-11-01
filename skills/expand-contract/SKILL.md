---
name: expand-contract
description: Plan zero-downtime schema, API, and event changes as expand, migrate, and contract steps that stay compatible during rolling deploys.
license: MIT
---

# Expand and contract

Use this when changing a live schema, API, event, or queue format. During a rolling deploy, a canary, or a rollback, old and new code run at the same time against the same data, so every intermediate state must work with both versions. A migration that's correct only after every process restarts causes an outage. That's why `ALTER TABLE ... RENAME COLUMN` is unsafe however fast it runs: the moment it commits, running code breaks.

For wire compatibility of protobuf schemas, use `protobuf-contracts`.

## Three phases

1. **Expand.** Add the new thing. Additive only, with old code unaware of it.
2. **Migrate.** Write both, backfill, move readers to the new thing, then stop writing the old one.
3. **Contract.** Remove the old thing once nothing reads or writes it.

Each phase is at least one deploy, and each can be released and reverted alone. Don't compress the gap between them, because that's where problems surface.

## Postgres recipes

Set a lock timeout before any DDL. A DDL statement waiting for a lock blocks every query arriving behind it, including plain `SELECT`s, so one `ALTER` behind one long transaction stalls the whole table. Failing fast lets you retry at a quieter moment.

```sql
SET lock_timeout = '3s';
ALTER TABLE orders ADD COLUMN region text;   -- SQLSTATE 55P03 instead of building a queue
```

**Rename a column.** Stopping between any two steps leaves a correct system.

| # | Step | Type |
| --- | --- | --- |
| 1 | Add the new column, nullable | Schema |
| 2 | Write both columns everywhere | Code |
| 3 | Backfill in batches | Script |
| 4 | Read the new column, keep writing both | Code |
| 5 | Stop writing the old column | Code |
| 6 | Drop the old column, once a rollback is no longer likely | Schema |

**Add a NOT NULL column.** A validated check constraint lets `SET NOT NULL` skip its table scan, so the constraint can be dropped afterwards.

```sql
ALTER TABLE users ADD COLUMN plan text;
-- deploy code that writes plan on every new row, then backfill in batches
UPDATE users SET plan = 'free' WHERE plan IS NULL AND id BETWEEN $1 AND $2;
ALTER TABLE users ADD CONSTRAINT users_plan_nn CHECK (plan IS NOT NULL) NOT VALID;
ALTER TABLE users VALIDATE CONSTRAINT users_plan_nn;
ALTER TABLE users ALTER COLUMN plan SET NOT NULL;
ALTER TABLE users DROP CONSTRAINT users_plan_nn;
```

| Change | Safe approach |
| --- | --- |
| Add an index | `CREATE INDEX CONCURRENTLY`. It can't run in a transaction, and a failed build leaves an `INVALID` index to drop |
| Change a column type | Treat it as a rename: new column, write both, backfill, switch, drop |
| Add a foreign key | `ADD CONSTRAINT ... NOT VALID`, then `VALIDATE CONSTRAINT` separately |
| Drop a column | Stop reading, deploy. Stop writing, deploy. Then drop |
| Add a volatile default | Add it nullable, backfill, then set the default for new rows |

Migration tools usually wrap each file in a transaction, which `CREATE INDEX CONCURRENTLY` rejects, so check how yours disables that.

## Backfills

- Update a few thousand rows per statement, with a pause between batches.
- Never hold one long transaction, because it blocks vacuum and grows replication lag.
- Track the last ID processed, so the job can resume and repeat safely.
- Throttle on replica lag rather than on wall-clock time.

## API and event changes

| Change | Safe approach |
| --- | --- |
| Add a field | Add it. Consumers must ignore unknown fields, and the contract must say so |
| Rename a field | Emit both, wait for consumers, remove the old one |
| Remove a field | Measure use first, deprecate, then remove |
| Change a type or meaning | Add a new field with a new name |
| Tighten validation | Log what would be rejected for a full cycle before rejecting it |

Never repurpose a name. A field that disappears is a clear error. A field whose meaning changed breaks quietly and wrongly.

A queue holds messages from the previous version for its whole retention window, so consumers must handle both shapes until that window passes after the last old producer stops.

## Prove it's unused before deleting

"Nobody uses it" without a metric means "nobody I asked". Monthly and quarterly jobs appear right after the column goes.

- **API field:** count reads per field by client, over a full business cycle.
- **Column:** check query logs and search every repository, not just this one.
- **Endpoint:** measure by caller identity, not total request count.

## Consumer habits

Don't use `SELECT *` in application code, since a new column changes the result shape. Don't rely on column order or JSON key order. Ignore unknown fields, and be strict only about the fields you use.

_Adapted from `coding-skills` by auralshin. The recipes were run against Postgres 17, which added the lock timeout example and the final constraint drop._
