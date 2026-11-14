# Idempotency keys

Use this when a retried or redelivered operation must take effect once. Accepting a key is the contract. Honoring it is where the money is lost, because a key handled carelessly is worse than none: the client now believes retrying is safe.

**Derive the key from the intent, not the attempt.** It must stay the same across retries of one intent and differ across distinct intents. A new UUID per attempt makes every retry a new charge. `userID:amount` merges two legitimate charges of the same size. A key with a timestamp is a new key per attempt. Use one the client generates once and reuses, or one derived from an immutable identifier such as `charge:v1:<orderID>`. It must come from the client or the initiating event, never from the layer doing the retrying.

**Claim atomically.** Checking and then acting is a race, because two concurrent retries can both see "not seen". Let the unique constraint pick the winner:

```go
func claim(ctx context.Context, db *sql.DB, key, requestHash string) (bool, error) {
	res, err := db.ExecContext(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, state)
		VALUES ($1, $2, 'in_progress')
		ON CONFLICT (key) DO NOTHING`, key, requestHash)
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	won, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	return won == 1, nil
}
```

The constraint is the mechanism. A store that can't enforce uniqueness in one operation can't back this.

**Guard the payload.** The same key with a different body is a client bug. Compare the stored request hash and fail loudly, rather than serving the first response to a second request.

**Decide what an in-flight duplicate gets.** The first request is often still running when the retry arrives.

| Strategy | Response | Use when |
| --- | --- | --- |
| Reject | HTTP 409 or gRPC `ABORTED` | The client can retry later. This is the simplest and safest |
| Wait | Block for the result, with a bound | The caller needs it synchronously |
| Return pending | HTTP 202 with a status URL | Effects run for a long time |

Never let the second caller through because the first seems stuck. An attempt whose fate is unknown is exactly when a duplicate costs most.

**Every call has three outcomes:** success, failure, and unknown. A timeout says nothing about whether the effect applied. Record the intent before calling out, so a crash between the call and the response leaves evidence for reconciliation rather than a silent retry.

**Set retention from the longest retry path,** not from storage cost. Keys must outlive every route that can redeliver the intent, including a dead-letter queue replayed a week later. A 24-hour key behind a seven-day queue is a duplicate waiting to happen.

_Adapted from `api-and-interface-design` in `addyosmani/agent-skills`, and rewritten for Go._
