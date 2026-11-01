# Index design

Use this when choosing or auditing PostgreSQL indexes. Verify every index decision with `EXPLAIN (ANALYZE, BUFFERS)`. An index that never appears in a plan is pure write overhead.

## Composite column order

A B-tree on `(a, b, c)` sorts by `a`, then `b` within equal `a`, then `c`. It serves `WHERE a = ?` and `WHERE a = ? AND b = ?`, but not `WHERE b = ?` alone. Once a range predicate is used on a column, later columns can only filter, not seek.

> Equality columns first, then the range or sort column, then the rest.

"Most selective column first" is usually wrong. Which columns are compared by equality matters far more than cardinality. A low-cardinality `status` belongs first when every query filters on it and ranges on a date.

```sql
SELECT * FROM events
WHERE tenant_id = 42 AND created_at > now() - interval '2 days'
ORDER BY created_at DESC;

CREATE INDEX events_tenant_created ON events (tenant_id, created_at DESC);
```

On a 200,000-row table, this query read 4 index pages with that index and 664 with `(created_at DESC, tenant_id)`, for the same 346 rows.

## Keeping predicates usable

Wrapping an indexed column usually prevents a seek, with no error and no warning.

| Not usable | Usable |
| --- | --- |
| `WHERE lower(email) = 'a@b.c'` | An expression index on `lower(email)` |
| `WHERE date(created_at) = '2026-01-01'` | `WHERE created_at >= '2026-01-01' AND created_at < '2026-01-02'` |
| `WHERE created_at + interval '1 day' > now()` | `WHERE created_at > now() - interval '1 day'` |
| `WHERE user_id::text = $1` | Compare as the column's own type |
| `WHERE id = 5.0` on a `bigint` | `WHERE id = 5` |

The last two are the sneakiest. A `numeric` value casts the column, so the plan silently becomes a sequential scan. A `text`-typed value against `bigint` fails with an error instead, and a quoted literal is coerced to `bigint` and uses the index.

`OR` across different columns usually prevents a single seek. Rewrite `WHERE a = 1 OR b = 2` as a `UNION` of two indexed queries.

## Covering and partial indexes

Use `INCLUDE` for columns needed only in the output, so the query can skip the table. An index-only scan still consults the visibility map, so an unvacuumed table shows high `Heap Fetches` and loses most of the benefit.

```sql
CREATE INDEX events_tenant_cover ON events (tenant_id, created_at) INCLUDE (status);
```

A partial index holds only the rows queried, which makes it smaller and cheaper to write. The planner uses it only when the query's `WHERE` implies the index predicate. It also keeps uniqueness alongside soft deletes:

```sql
CREATE UNIQUE INDEX users_live_email ON users (email) WHERE deleted_at IS NULL;
```

## Primary keys

A B-tree stays cheap when inserts land at its right-hand edge. Random UUIDv4 keys touch a random page on each insert, split pages early, and make the whole index the working set, and every secondary index inherits the cost. Use time-ordered identifiers such as UUIDv7 for new tables. Existing UUIDv4 keys rarely justify a migration on their own.

## Costs and auditing

Every index slows writes. An `UPDATE` touches every index whose columns changed, and all of them when it can't be a heap-only update. On write-heavy tables, each index needs a named query that justifies it.

An index on `(a)` is redundant when `(a, b)` exists. When checking `idx_scan` for unused indexes, also check replicas, and cover a full business cycle, because monthly jobs use indexes nothing else touches.

Build indexes on live tables with `CREATE INDEX CONCURRENTLY`, as `expand-contract` describes.

_Adapted from `coding-skills` by auralshin. Measurements and the type-mismatch rows come from runs against Postgres 17._
