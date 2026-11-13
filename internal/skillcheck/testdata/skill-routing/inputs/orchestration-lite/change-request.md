# Retry outbound calls

1. Retry a call that fails with a timeout, a connection reset or an HTTP 502, 503 or 504.
2. Make at most 4 attempts. Wait 100 ms, 200 ms and 400 ms between them, each plus up to 50 ms of jitter.
3. Never retry an HTTP 4xx response or a context cancellation.
4. When the attempts run out, return the last error wrapped as `ErrRetriesExhausted`, with the attempt count.
5. Apply the same rules in `httpclient`, `queue/consumer` and `storage/adapter`, on Linux and macOS.
