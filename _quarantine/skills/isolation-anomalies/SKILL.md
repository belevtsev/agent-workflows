---
name: isolation-anomalies
description: Find and fix concurrency anomalies such as write skew and lost updates from transaction isolation levels.
license: MIT
---

# Transaction isolation anomalies

Use this when choosing an isolation level, when an invariant breaks only under concurrent traffic, or when reviewing code that reads rows and then writes based on them. For transaction syntax in Go, use `golang-database`.

Isolation levels aren't a ladder of how much locking you get. They're defined by which anomalies they permit, and the same label means different things across databases. Postgres `REPEATABLE READ` is snapshot isolation, and Oracle's `SERIALIZABLE` is too. Reason from the anomaly, not from the label.

| Anomaly | READ COMMITTED | Postgres REPEATABLE READ | SERIALIZABLE |
| --- | --- | --- | --- |
| Dirty read | Prevented | Prevented | Prevented |
| Non-repeatable read | **Possible** | Prevented | Prevented |
| Phantom read | **Possible** | Prevented | Prevented |
| Lost update | **Possible** | Detected, aborts | Prevented |
| **Write skew** | **Possible** | **Possible** | Prevented |

## Write skew

Snapshot isolation prevents everything people usually think of, and still permits write skew. Take the rule that at least one doctor stays on call, with two on call now:

```sql
-- Sessions A and B run this at the same time, with different ids
BEGIN ISOLATION LEVEL REPEATABLE READ;
SELECT count(*) FROM doctors WHERE on_call AND shift_id = 42;  -- both see 2
UPDATE doctors SET on_call = false WHERE id = 1;               -- B updates id 2
COMMIT;
```

Both commit with no error, and nobody is on call. Neither wrote a row the other read, so nothing conflicts. Run under `SERIALIZABLE`, one of the two fails instead.

Suspect write skew whenever a transaction **reads** a set of rows, **decides** based on what it read, and **writes** something that would have changed that decision had it been visible. Treat that shape as broken until fixed. Typical cases are booking without overlap, balance checks across accounts, unique names without a unique index, quotas, and "only one primary" flags.

## Fixes, in order of preference

**1. Let a constraint enforce it.** This is the cheapest and most reliable fix, and it holds regardless of isolation level or which code path writes.

```sql
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- btree_gist is needed for the equality on a plain column
CREATE EXTENSION IF NOT EXISTS btree_gist;
ALTER TABLE bookings ADD EXCLUDE USING gist (room_id WITH =, during WITH &&);
```

**2. Make the write atomic.** Reading a value and writing it back from application code is a lost update. Put the arithmetic and the check in one statement, and check the row count:

```sql
UPDATE accounts SET balance = balance - 10 WHERE id = 1 AND balance >= 10;
-- 0 rows means insufficient funds
```

**3. Lock one parent row.** Locking a row that represents the invariant turns the race into a queue, and it's often simpler than locking a set. Postgres rejects `FOR UPDATE` on an aggregate, so a lock can't be added to the `count(*)` above.

```sql
BEGIN;
SELECT id FROM shifts WHERE id = 42 FOR UPDATE;
SELECT count(*) FROM doctors WHERE on_call AND shift_id = 42;
UPDATE doctors SET on_call = false WHERE id = 1;
COMMIT;
```

Every writer of that invariant must take the same lock. Take locks in a consistent order across the codebase, or you've traded write skew for deadlocks.

**4. Use SERIALIZABLE.** Postgres runs this optimistically and aborts a transaction that would break serializability, so every use needs a retry loop:

```go
// isConflict reports a serialization failure or deadlock. pgx and lib/pq errors
// both expose SQLState.
func isConflict(err error) bool {
	var state interface{ SQLState() string }
	if !errors.As(err, &state) {
		return false
	}
	return state.SQLState() == "40001" || state.SQLState() == "40P01"
}

// inSerializable retries fn on conflict, so fn must be safe to run again.
func inSerializable(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	var err error
	for attempt := range 5 {
		err = runTx(ctx, db, &sql.TxOptions{Isolation: sql.LevelSerializable}, fn)
		if !isConflict(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rand.N(10 * time.Millisecond << attempt)):
		}
	}
	return fmt.Errorf("serializable transaction: retries exhausted: %w", err)
}

func runTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
```

The transaction body must have no effects outside the database until after commit. That's the usual way this goes wrong: not throughput, but a retried transaction that sends the email twice.

## READ COMMITTED traps

Most systems run here, so these appear constantly:

- Two reads in one transaction can disagree, so a report built from several queries can contradict itself.
- A blocked `UPDATE` re-checks its `WHERE` against the newly committed row, so the predicate may no longer match. Check the row count.
- A `SELECT` followed by an `UPDATE` in application code is a lost-update risk. Use a conditional update or a lock.

For long user-facing edits, use a version column instead of holding a lock:

```sql
UPDATE docs SET body = $1, version = version + 1 WHERE id = $2 AND version = $3;
-- 0 rows means someone else edited it first
```

## Choosing

Default to `READ COMMITTED` with constraints and conditional updates, which covers most applications. Use `SERIALIZABLE` for multi-row invariants such as accounting, scheduling, and quotas, and accept the retry loop.

## Prove it with a concurrent test

These bugs never appear in single-threaded tests, and a test that happens to interleave badly is flaky. Use a barrier so both transactions finish reading before either writes, run it against the real database, then assert the invariant. Without that test you can't show the fix works.

```go
// waitForPeer runs after a transaction's read and before its write. Create reads
// with a count of 2, so neither transaction can write until both have read.
func waitForPeer(reads *sync.WaitGroup) {
	reads.Done()
	reads.Wait()
}
```

With a retry loop, release the barrier on the first attempt only, because waiting on it twice hangs. Name the anomaly in review, since "this is write skew under snapshot isolation" ends the discussion.

_Adapted from `coding-skills` by auralshin. The examples were run against Postgres 17, which replaced the upstream `FOR UPDATE` example and added `btree_gist`._
