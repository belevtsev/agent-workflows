---
name: db-postgres
description: Diagnose PostgreSQL query plans, indexes, JSONB, replication, or maintenance using the actual schema, workload, server version, and operational constraints.
license: MIT
metadata:
  author: https://github.com/Jeffallan
  upstream-version: "1.1.0"
disable-model-invocation: true
---

# PostgreSQL diagnosis

Resolve this installed skill and references relative to its directory. Establish the PostgreSQL version, managed-service restrictions, permitted changes, and evidence relevant to the request. Use existing schema, queries, indexes, plans, and statistics before asking for information already available.

Read only the applicable reference:

- Query plans, index choice, statistics: [performance](references/performance.md).
- JSONB operators and indexing: [JSONB](references/jsonb.md).
- Extension availability and operator/index behavior: [extensions](references/extensions.md).
- Lag, slots, WAL retention, and failover: [replication](references/replication.md).
- VACUUM, bloat, long transactions, and monitoring: [maintenance](references/maintenance.md).

Match recommendations to observed predicates, ordering, selectivity, and write patterns. Verify version-sensitive behavior and provider support against the target environment and current official documentation. Keep confirmed observations distinct from hypotheses.

`EXPLAIN ANALYZE` executes the statement; even a SELECT may call functions with effects. Use plain EXPLAIN when execution is outside the authorized scope. A rollback does not undo every external effect or sequence advance. Do not execute production DDL, maintenance, or configuration changes merely to produce advice. When execution is authorized, account for locks, write amplification, disk growth, and rollback. Avoid disabling autovacuum globally or treating VACUUM FULL as routine online maintenance.

Deliver the diagnosis, exact proposed SQL or setting, evidence and expected effect, and relevant validation/rollback. Scale the response to the task; a simple query answer does not require a multi-section report.
